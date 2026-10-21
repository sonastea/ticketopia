package persistence

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// loadProfile attributes ordinary-traffic latency in the deployment fixture to
// database or application contention. Status and cgroup deltas are always read;
// LIVE_ACTIVITY_LOAD_PROFILE=true also enables a temporary global statement log
// on the isolated local server (never a shared database) and restores it.
type loadProfile struct {
	t          *testing.T
	f          mariaFixture
	db         string
	containers []string
	docker     *http.Client
	logFile    string
	status     map[string]float64
	cpu        map[string]containerCPU
	offset     int64
}

type containerCPU struct{ usage, periods, throttled, throttledTime float64 }

var loadStatusNames = []string{
	"Connections", "Ssl_accepts", "Threads_created", "Questions", "Com_stmt_prepare",
	"Innodb_row_lock_waits", "Innodb_row_lock_time", "Innodb_os_log_fsyncs",
	"Innodb_data_fsyncs", "Innodb_rows_read", "Innodb_buffer_pool_read_requests",
	"Created_tmp_tables", "Created_tmp_disk_tables", "Handler_read_rnd_next",
	"Innodb_history_list_length", "Threads_running",
}

func newLoadProfile(t *testing.T, f mariaFixture, dbContainer string) *loadProfile {
	p := &loadProfile{t: t, f: f, db: dbContainer}
	if host, err := exec.CommandContext(t.Context(), "docker", "context", "inspect", "--format", "{{.Endpoints.docker.Host}}").Output(); err == nil {
		socket := strings.TrimPrefix(strings.TrimSpace(string(host)), "unix://")
		p.docker = &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socket)
		}}}
	}
	if os.Getenv("LIVE_ACTIVITY_LOAD_PROFILE") != "true" {
		return p
	}
	// Session long_query_time is inherited at connect, so enable before the
	// replicas open their pools. Restore every changed global on cleanup.
	saved := map[string]string{}
	for _, name := range []string{"slow_query_log", "long_query_time", "slow_query_log_file", "log_slow_verbosity", "log_output"} {
		var key, value string
		if err := f.admin.QueryRowContext(t.Context(), "SHOW GLOBAL VARIABLES LIKE '"+name+"'").Scan(&key, &value); err != nil {
			t.Fatal(err)
		}
		saved[name] = value
	}
	p.logFile = "/tmp/" + f.runtime.Database + ".slow.log"
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		enabled := "0"
		if saved["slow_query_log"] == "ON" {
			enabled = "1"
		}
		for _, q := range []string{
			"SET GLOBAL slow_query_log=" + enabled,
			"SET GLOBAL long_query_time=" + saved["long_query_time"],
			"SET GLOBAL log_slow_verbosity='" + saved["log_slow_verbosity"] + "'",
			"SET GLOBAL log_output='" + saved["log_output"] + "'",
			"SET GLOBAL slow_query_log_file='" + saved["slow_query_log_file"] + "'",
		} {
			if _, err := f.admin.ExecContext(ctx, q); err != nil {
				t.Error("restore profile setting", err)
			}
		}
		exec.Command("docker", "exec", dbContainer, "rm", "-f", p.logFile).Run()
	})
	for _, q := range []string{
		"SET GLOBAL log_output='FILE'",
		"SET GLOBAL slow_query_log_file='" + p.logFile + "'",
		"SET GLOBAL log_slow_verbosity='query_plan,innodb'",
		"SET GLOBAL long_query_time=0",
		"SET GLOBAL slow_query_log=1",
	} {
		execSQL(t, f.admin, q)
	}
	return p
}

func (p *loadProfile) readStatus() map[string]float64 {
	result := map[string]float64{}
	rows, err := p.f.admin.QueryContext(p.t.Context(), "SHOW GLOBAL STATUS")
	if err != nil {
		p.t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var name, value string
		if err := rows.Scan(&name, &value); err != nil {
			p.t.Fatal(err)
		}
		if slices.Contains(loadStatusNames, name) {
			result[name], _ = strconv.ParseFloat(value, 64)
		}
	}
	return result
}

func (p *loadProfile) readCPU() map[string]containerCPU {
	result := map[string]containerCPU{}
	if p.docker == nil {
		return result
	}
	for _, name := range append([]string{p.db}, p.containers...) {
		resp, err := p.docker.Get("http://docker/containers/" + name + "/stats?stream=false&one-shot=true")
		if err != nil {
			continue
		}
		var stats struct {
			CPU struct {
				Usage struct {
					Total float64 `json:"total_usage"`
				} `json:"cpu_usage"`
				Throttling struct {
					Periods       float64 `json:"periods"`
					Throttled     float64 `json:"throttled_periods"`
					ThrottledTime float64 `json:"throttled_time"`
				} `json:"throttling_data"`
			} `json:"cpu_stats"`
		}
		err = json.NewDecoder(resp.Body).Decode(&stats)
		resp.Body.Close()
		if err == nil {
			c := stats.CPU
			result[name] = containerCPU{c.Usage.Total, c.Throttling.Periods, c.Throttling.Throttled, c.Throttling.ThrottledTime}
		}
	}
	return result
}

func (p *loadProfile) logSize() int64 {
	if p.logFile == "" {
		return 0
	}
	out, err := exec.CommandContext(p.t.Context(), "docker", "exec", p.db, "stat", "-c", "%s", p.logFile).Output()
	if err != nil {
		return 0
	}
	n, _ := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	return n
}

func (p *loadProfile) begin() {
	p.status, p.cpu, p.offset = p.readStatus(), p.readCPU(), p.logSize()
}

func (p *loadProfile) end(label string, elapsed time.Duration) {
	t := p.t
	status, cpu, offset := p.readStatus(), p.readCPU(), p.logSize()
	parts := []string{}
	for _, name := range loadStatusNames {
		if name == "Innodb_history_list_length" || name == "Threads_running" {
			parts = append(parts, fmt.Sprintf("%s=%.0f", name, status[name]))
			continue
		}
		parts = append(parts, fmt.Sprintf("%s=+%.0f", name, status[name]-p.status[name]))
	}
	t.Logf("profile %s mariadb status: %s", label, strings.Join(parts, " "))
	for name, after := range cpu {
		before := p.cpu[name]
		t.Logf("profile %s cpu %s: %.2f cores, throttled %.0f/%.0f periods (%.1f ms)", label, name,
			(after.usage-before.usage)/float64(elapsed.Nanoseconds()), after.throttled-before.throttled,
			after.periods-before.periods, (after.throttledTime-before.throttledTime)/1e6)
	}
	if p.logFile == "" || offset <= p.offset {
		return
	}
	out, err := exec.CommandContext(t.Context(), "docker", "exec", p.db, "sh", "-c",
		fmt.Sprintf("tail -c +%d %s | head -c %d", p.offset+1, p.logFile, offset-p.offset)).Output()
	if err != nil {
		t.Log("profile log unavailable", err)
		return
	}
	reportStatements(t, label, p.f.runtime.User, string(out))
}

type loggedStatement struct {
	thread       string
	query        string
	seconds      float64
	lock         float64
	oldRows      float64
	rowsExamined float64
}

var (
	slowHeader  = regexp.MustCompile(`(\w+): ([0-9.]+)`)
	slowLiteral = regexp.MustCompile(`'(?:[^'\\]|\\.)*'|\b\d+(?:\.\d+)?\b`)
	slowList    = regexp.MustCompile(`\((?:\?,?\s*)+\)`)
)

func parseSlowLog(user, log string) []loggedStatement {
	var result []loggedStatement
	for _, entry := range strings.Split(log, "# User@Host: ")[1:] {
		lines := strings.Split(entry, "\n")
		if !strings.HasPrefix(lines[0], user+"[") {
			continue
		}
		s := loggedStatement{}
		body := []string{}
		for _, line := range lines[1:] {
			switch {
			case strings.HasPrefix(line, "# Time:"):
			case strings.HasPrefix(line, "# "):
				for _, m := range slowHeader.FindAllStringSubmatch(line, -1) {
					v, _ := strconv.ParseFloat(m[2], 64)
					switch m[1] {
					case "Thread_id":
						s.thread = m[2]
					case "Query_time":
						s.seconds = v
					case "Lock_time":
						s.lock = v
					case "Old_rows_read":
						s.oldRows = v
					case "Rows_examined":
						s.rowsExamined = v
					}
				}
			case strings.HasPrefix(line, "SET timestamp="), strings.HasPrefix(line, "use "):
			default:
				body = append(body, line)
			}
		}
		s.query = strings.TrimSpace(strings.Join(body, " "))
		result = append(result, s)
	}
	return result
}

func fingerprint(query string) string {
	q := slowLiteral.ReplaceAllString(query, "?")
	q = slowList.ReplaceAllString(q, "(?…)")
	q = strings.Join(strings.Fields(q), " ")
	if len(q) > 110 {
		q = q[:110]
	}
	return q
}

func percentile(values []float64, p int) float64 {
	if len(values) == 0 {
		return 0
	}
	values = slices.Clone(values)
	slices.Sort(values)
	return values[(len(values)*p-1)/100]
}

func reportStatements(t *testing.T, label, user, log string) {
	statements := parseSlowLog(user, log)
	type aggregate struct {
		times               []float64
		lock, old, examined float64
	}
	byFingerprint := map[string]*aggregate{}
	for _, s := range statements {
		key := fingerprint(s.query)
		a := byFingerprint[key]
		if a == nil {
			a = &aggregate{}
			byFingerprint[key] = a
		}
		a.times = append(a.times, s.seconds)
		a.lock += s.lock
		a.old += s.oldRows
		a.examined += s.rowsExamined
	}
	keys := []string{}
	total := map[string]float64{}
	for key, a := range byFingerprint {
		keys = append(keys, key)
		for _, v := range a.times {
			total[key] += v
		}
	}
	slices.SortFunc(keys, func(a, b string) int { return int((total[b] - total[a]) * 1e9) })
	t.Logf("profile %s statements=%d fingerprints=%d", label, len(statements), len(keys))
	for _, key := range keys[:min(14, len(keys))] {
		a := byFingerprint[key]
		t.Logf("profile %s sql n=%d total=%.1fms p50=%.3fms p95=%.3fms max=%.3fms lock=%.1fms old_rows=%.0f examined=%.0f %s", label, len(a.times), total[key]*1e3,
			percentile(a.times, 50)*1e3, percentile(a.times, 95)*1e3, percentile(a.times, 100)*1e3, a.lock*1e3, a.old, a.examined, key)
	}
	// Reconstruct each ordinary write transaction from its connection's sequence.
	open := map[string][]loggedStatement{}
	var txTimes, commitTimes, slowest []float64
	slowestKind := map[string]int{}
	for _, s := range statements {
		upper := strings.TrimSuffix(strings.ToUpper(s.query), ";")
		if strings.HasPrefix(upper, "START TRANSACTION") || strings.HasPrefix(upper, "BEGIN") {
			open[s.thread] = []loggedStatement{s}
			continue
		}
		tx, ok := open[s.thread]
		if !ok {
			continue
		}
		tx = append(tx, s)
		open[s.thread] = tx
		if upper != "COMMIT" && upper != "ROLLBACK" {
			continue
		}
		delete(open, s.thread)
		write := false
		for _, statement := range tx {
			if strings.Contains(statement.query, "event_interests") && (strings.HasPrefix(statement.query, "INSERT") || strings.HasPrefix(statement.query, "DELETE")) {
				write = true
			}
		}
		if !write {
			continue
		}
		sum, worst, worstKind := 0.0, 0.0, ""
		for _, statement := range tx {
			sum += statement.seconds
			if statement.seconds > worst {
				worst, worstKind = statement.seconds, fingerprint(statement.query)
			}
		}
		txTimes = append(txTimes, sum)
		commitTimes = append(commitTimes, s.seconds)
		slowest = append(slowest, worst)
		slowestKind[worstKind]++
	}
	t.Logf("profile %s write transactions=%d server_sql p50=%.3fms p95=%.3fms max=%.3fms; commit p50=%.3fms p95=%.3fms; slowest_statement p95=%.3fms",
		label, len(txTimes), percentile(txTimes, 50)*1e3, percentile(txTimes, 95)*1e3, percentile(txTimes, 100)*1e3,
		percentile(commitTimes, 50)*1e3, percentile(commitTimes, 95)*1e3, percentile(slowest, 95)*1e3)
	for kind, n := range slowestKind {
		if n >= max(2, len(txTimes)/20) {
			t.Logf("profile %s slowest-in-write n=%d %s", label, n, kind)
		}
	}
}
