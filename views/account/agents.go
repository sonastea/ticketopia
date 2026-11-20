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

// agentIntro is the guide's one intro line for what this deployment offers.
func agentIntro(p AgentGuidePage) string {
	switch {
	case p.OAuth:
		return "Connect an AI agent through MCP to choose what it can do for you. Use an API token for scripts and apps you run yourself."
	case p.Accounts:
		return "Use an API token for scripts and apps you run yourself."
	case p.MCP:
		return "Find events and read public information with the agent tools or the JSON API."
	}
	return "Find events and read public information with the JSON API."
}
