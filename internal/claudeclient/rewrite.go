package claudeclient

import (
	"encoding/base64"
	"encoding/json"
	"math/rand"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var (
	billingVersionPattern = regexp.MustCompile(`cc_version=[\d.]+\.[a-f0-9]{3}`)
	platformPattern       = regexp.MustCompile(`Platform:\s*\S+`)
	shellPattern          = regexp.MustCompile(`Shell:\s*\S+`)
	osVersionPattern      = regexp.MustCompile(`OS Version:\s*[^\n<]+`)
	workDirPattern        = regexp.MustCompile(`((?:Primary )?[Ww]orking directory:\s*)/\S+`)
	homePathPattern       = regexp.MustCompile(`/((?:Users|home))/[^/\s]+/`)
)

func RewriteBody(body []byte, path string, config Config) []byte {
	var parsed any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return body
	}

	switch {
	case strings.HasPrefix(path, "/v1/messages"):
		rewriteMessagesBody(parsed, config)
	case strings.Contains(path, "/event_logging/batch"):
		rewriteEventBatch(parsed, config)
	case strings.Contains(path, "/policy_limits"), strings.Contains(path, "/settings"):
		rewriteGenericIdentity(parsed, config)
	}

	rewritten, err := json.Marshal(parsed)
	if err != nil {
		return body
	}
	return rewritten
}

func RewriteHeaders(headers http.Header, config Config, accessToken string) http.Header {
	out := make(http.Header)

	for key, values := range headers {
		lower := strings.ToLower(strings.TrimSpace(key))
		switch lower {
		case "host", "connection", "proxy-authorization", "proxy-connection", "transfer-encoding", "authorization", "content-length":
			continue
		case "user-agent":
			out.Set("User-Agent", "claude-code/"+config.Env.Version+" (external, cli)")
		case "x-anthropic-billing-header":
			out.Set("X-Anthropic-Billing-Header", billingVersionPattern.ReplaceAllString(strings.Join(values, ", "), "cc_version="+config.Env.Version+".000"))
		default:
			for _, value := range values {
				out.Add(key, value)
			}
		}
	}

	out.Set("Authorization", "Bearer "+accessToken)
	return out
}

func BuildVerificationPayload(config Config) map[string]any {
	sampleInput := map[string]any{
		"metadata": map[string]any{
			"user_id": `{"device_id":"REAL_DEVICE_ID_FROM_CLIENT_abc123","account_uuid":"shared-account-uuid","session_id":"session-xxx"}`,
		},
		"system": []map[string]any{
			{
				"type": "text",
				"text": "x-anthropic-billing-header: cc_version=2.1.81.a1b; cc_entrypoint=cli;",
			},
			{
				"type": "text",
				"text": "Here is useful information about the environment:\n<env>\nWorking directory: /home/bob/myproject\nPlatform: linux\nShell: bash\nOS Version: Linux 6.5.0-generic\n</env>",
			},
		},
		"messages": []map[string]any{
			{"role": "user", "content": "hello"},
		},
	}

	beforeUserID := map[string]any{}
	_ = json.Unmarshal([]byte(sampleInput["metadata"].(map[string]any)["user_id"].(string)), &beforeUserID)
	rewritten := map[string]any{}
	_ = json.Unmarshal(RewriteBody(mustJSON(sampleInput), "/v1/messages", config), &rewritten)

	afterUserID := map[string]any{}
	metadata := rewritten["metadata"].(map[string]any)
	_ = json.Unmarshal([]byte(metadata["user_id"].(string)), &afterUserID)
	system := rewritten["system"].([]any)

	return map[string]any{
		"_info": "This shows how the gateway rewrites a sample request",
		"before": map[string]any{
			"metadata.user_id":  beforeUserID,
			"system_prompt_env": sampleInput["system"].([]map[string]any)[1]["text"],
			"billing_header":    sampleInput["system"].([]map[string]any)[0]["text"],
		},
		"after": map[string]any{
			"metadata.user_id":  afterUserID,
			"system_prompt_env": system[1].(map[string]any)["text"],
			"billing_header":    system[0].(map[string]any)["text"],
		},
	}
}

func mustJSON(value any) []byte {
	content, _ := json.Marshal(value)
	return content
}

func rewriteMessagesBody(body any, config Config) {
	root, ok := body.(map[string]any)
	if !ok {
		return
	}

	if metadata, ok := root["metadata"].(map[string]any); ok {
		if raw, ok := metadata["user_id"].(string); ok && raw != "" {
			var userID map[string]any
			if err := json.Unmarshal([]byte(raw), &userID); err == nil {
				userID["device_id"] = config.Identity.DeviceID
				if config.Identity.Email != "" {
					userID["email"] = config.Identity.Email
				}
				metadata["user_id"] = string(mustJSON(userID))
			}
		}
	}

	switch system := root["system"].(type) {
	case string:
		root["system"] = rewritePromptText(system, config)
	case []any:
		for _, item := range system {
			if block, ok := item.(map[string]any); ok {
				if text, ok := block["text"].(string); ok {
					block["text"] = rewritePromptText(text, config)
				}
			}
		}
	}

	if messages, ok := root["messages"].([]any); ok {
		for _, entry := range messages {
			message, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			switch content := message["content"].(type) {
			case string:
				message["content"] = rewritePromptText(content, config)
			case []any:
				for _, block := range content {
					if value, ok := block.(map[string]any); ok {
						if text, ok := value["text"].(string); ok {
							value["text"] = rewritePromptText(text, config)
						}
					}
				}
			}
		}
	}
}

func rewritePromptText(text string, config Config) string {
	result := text
	result = billingVersionPattern.ReplaceAllString(result, "cc_version="+config.Env.Version+".000")
	result = platformPattern.ReplaceAllString(result, "Platform: "+config.PromptEnv.Platform)
	result = shellPattern.ReplaceAllString(result, "Shell: "+config.PromptEnv.Shell)
	result = osVersionPattern.ReplaceAllString(result, "OS Version: "+config.PromptEnv.OSVersion)
	result = workDirPattern.ReplaceAllString(result, "${1}"+config.PromptEnv.WorkingDir)

	homePrefix := "/Users/user/"
	if matches := regexp.MustCompile(`^/[^/]+/[^/]+/`).FindString(config.PromptEnv.WorkingDir + "/"); matches != "" {
		homePrefix = matches
	}
	result = homePathPattern.ReplaceAllString(result, homePrefix)
	return result
}

func rewriteEventBatch(body any, config Config) {
	root, ok := body.(map[string]any)
	if !ok {
		return
	}
	events, ok := root["events"].([]any)
	if !ok {
		return
	}

	for _, entry := range events {
		event, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		data, ok := event["event_data"].(map[string]any)
		if !ok {
			continue
		}
		if _, ok := data["device_id"]; ok {
			data["device_id"] = config.Identity.DeviceID
		}
		if _, ok := data["email"]; ok && config.Identity.Email != "" {
			data["email"] = config.Identity.Email
		}
		if _, ok := data["env"]; ok {
			data["env"] = buildCanonicalEnv(config)
		}
		if original, ok := data["process"]; ok {
			data["process"] = buildCanonicalProcess(original, config)
		}
		delete(data, "baseUrl")
		delete(data, "base_url")
		delete(data, "gateway")
		if raw, ok := data["additional_metadata"].(string); ok && raw != "" {
			data["additional_metadata"] = rewriteAdditionalMetadata(raw)
		}
	}
}

func rewriteGenericIdentity(body any, config Config) {
	root, ok := body.(map[string]any)
	if !ok {
		return
	}
	if _, ok := root["device_id"]; ok {
		root["device_id"] = config.Identity.DeviceID
	}
	if _, ok := root["email"]; ok && config.Identity.Email != "" {
		root["email"] = config.Identity.Email
	}
}

func buildCanonicalEnv(config Config) map[string]any {
	return map[string]any{
		"platform":               config.Env.Platform,
		"platform_raw":           config.Env.PlatformRaw,
		"arch":                   config.Env.Arch,
		"node_version":           config.Env.NodeVersion,
		"terminal":               config.Env.Terminal,
		"package_managers":       config.Env.PackageManagers,
		"runtimes":               config.Env.Runtimes,
		"is_running_with_bun":    config.Env.IsRunningWithBun,
		"is_ci":                  false,
		"is_claubbit":            false,
		"is_claude_code_remote":  false,
		"is_local_agent_mode":    false,
		"is_conductor":           false,
		"is_github_action":       false,
		"is_claude_code_action":  false,
		"is_claude_ai_auth":      config.Env.IsClaudeAIAuth,
		"version":                config.Env.Version,
		"version_base":           config.Env.VersionBase,
		"build_time":             config.Env.BuildTime,
		"deployment_environment": config.Env.DeploymentEnvironment,
		"vcs":                    config.Env.VCS,
	}
}

func buildCanonicalProcess(original any, config Config) any {
	switch value := original.(type) {
	case string:
		decoded, err := base64.StdEncoding.DecodeString(value)
		if err != nil {
			return original
		}
		var process map[string]any
		if err := json.Unmarshal(decoded, &process); err != nil {
			return original
		}
		rewritten := rewriteProcessFields(process, config)
		content, err := json.Marshal(rewritten)
		if err != nil {
			return original
		}
		return base64.StdEncoding.EncodeToString(content)
	case map[string]any:
		return rewriteProcessFields(value, config)
	default:
		return original
	}
}

func rewriteProcessFields(process map[string]any, config Config) map[string]any {
	out := make(map[string]any, len(process)+4)
	for key, value := range process {
		out[key] = value
	}
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	out["constrainedMemory"] = config.Process.ConstrainedMemory
	out["rss"] = randomInRange(rng, config.Process.RSSRange)
	out["heapTotal"] = randomInRange(rng, config.Process.HeapTotalRange)
	out["heapUsed"] = randomInRange(rng, config.Process.HeapUsedRange)
	return out
}

func rewriteAdditionalMetadata(raw string) string {
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return raw
	}
	var payload map[string]any
	if err := json.Unmarshal(decoded, &payload); err != nil {
		return raw
	}
	delete(payload, "baseUrl")
	delete(payload, "base_url")
	delete(payload, "gateway")
	content, err := json.Marshal(payload)
	if err != nil {
		return raw
	}
	return base64.StdEncoding.EncodeToString(content)
}

func randomInRange(rng *rand.Rand, bounds [2]int64) int64 {
	if bounds[1] <= bounds[0] {
		return bounds[0]
	}
	return bounds[0] + rng.Int63n(bounds[1]-bounds[0]+1)
}
