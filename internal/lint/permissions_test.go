package lint

import "testing"

func TestCovers(t *testing.T) {
	cases := []struct {
		name    string
		deny    string
		allow   string
		covered bool
	}{
		{"an identical prefix rule is covered", "Bash(git push*)", "Bash(git push*)", true},
		{"a narrower allow is covered", "Bash(git push*)", "Bash(git push origin*)", true},
		{"an exact allow inside the denied prefix is covered", "Bash(rm -rf *)", "Bash(rm -rf /tmp/x)", true},
		{"a broader allow is the except pattern and not covered", "Bash(git push*)", "Bash(git *)", false},
		{"a different command sharing letters is not covered", "Bash(git push*)", "Bash(git pusher*)", false},
		{"an unrelated command is not covered", "Bash(git add*)", "Bash(git status*)", false},
		{"a different tool is not covered", "Bash(git *)", "Read(git)", false},
		{"a bare tool deny covers every allow of that tool", "WebSearch", "WebSearch", true},
		{"an exact deny covers only the same exact allow", "Read(./.env)", "Read(./.env)", true},
		{"an exact deny does not cover a prefix allow", "Read(./.env)", "Read(./.env*)", false},
		{"the colon wildcard form is read like the space form", "Bash(git:*)", "Bash(git push*)", true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if covers(testCase.deny, testCase.allow) != testCase.covered {
				t.Fatalf("covers(%q, %q) = %v, expected %v", testCase.deny, testCase.allow, !testCase.covered, testCase.covered)
			}
		})
	}
}
