package netutil

import "testing"

func TestHostMatcherSuffixMatchesRootAndSubdomains(t *testing.T) {
	matcher := NewHostMatcher(nil, []string{".chatgpt.com"})

	cases := map[string]bool{
		"chatgpt.com":         true,
		"api.chatgpt.com":     true,
		"sub.api.chatgpt.com": true,
		"evilchatgpt.com":     false,
		"chatgpt.co":          false,
	}

	for host, want := range cases {
		if got := matcher.Match(host); got != want {
			t.Fatalf("Match(%q) = %v, want %v", host, got, want)
		}
	}
}
