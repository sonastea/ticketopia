package account

import "strings"

type AgentGuidePage struct {
	Origin                                       string
	Accounts, MCP, OAuth, SignInRequired, Drafts bool
}

func agentProfileCurl(baseURL string) string {
	return agentCommand("curl --header @-", "", baseURL+"/api/v1/me/profile") + ` <<EOF
Authorization: Bearer $TICKETOPIA_TOKEN
EOF`
}

func agentCommand(command, flag, url string) string {
	// Shell-quote the URL; HTML escaping alone does not protect shell syntax.
	return command + " \\\n  " + flag + "'" + strings.ReplaceAll(url, "'", `'"'"'`) + "'"
}
