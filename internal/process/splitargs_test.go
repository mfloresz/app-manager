package process

import (
	"slices"
	"testing"
)

// TestSplitArgs covers the shell-like tokenizer: plain args, repeated
// whitespace, quoted spaces, escapes, empty quoted args, adjacent quoting,
// literal metacharacters, and the documented malformed-input behavior.
func TestSplitArgs(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		want []string
	}{
		{"empty string", "", nil},
		{"plain args", "serve --port 8080", []string{"serve", "--port", "8080"}},
		{"repeated whitespace", "  a    b\tc\n d  ", []string{"a", "b", "c", "d"}},
		{"single quoted spaces", `--name 'John Doe'`, []string{"--name", "John Doe"}},
		{"double quoted spaces", `--name "John Doe"`, []string{"--name", "John Doe"}},
		{"empty double quoted arg", `--name ""`, []string{"--name", ""}},
		{"empty single quoted arg", `--name ''`, []string{"--name", ""}},
		{"two empty quoted args", `"" ""`, []string{"", ""}},
		{"quoted space only", `" "`, []string{" "}},
		{"escaped space", `foo\ bar`, []string{"foo bar"}},
		{"escaped double quote", `"a\"b"`, []string{`a"b`}},
		{"escaped backslash", `a\\b`, []string{`a\b`}},
		{"backslash literal in single quotes", `'a\b'`, []string{`a\b`}},
		{"equals with quoted space", `--name='John Doe'`, []string{"--name=John Doe"}},
		{"adjacent quoting", `a"b c"d`, []string{"ab cd"}},
		{"apostrophe in a word is unmatched quote", `it's`, []string{"its"}},
		{"unmatched double quote", `--name "John`, []string{"--name", "John"}},
		{"trailing backslash literal", `foo\`, []string{`foo\`}},
		{"dollar at end literal", `foo$`, []string{"foo$"}},
		{"metacharacters are literal", `a;b|c&d`, []string{"a;b|c&d"}},
		{"command substitution is literal", `--x $(echo hi)`, []string{"--x", "$(echo", "hi)"}},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			got := SplitArgs(tt.cmd)
			if !slices.Equal(got, tt.want) {
				t.Errorf("SplitArgs(%q) = %#v, want %#v", tt.cmd, got, tt.want)
			}
		})
	}
}

// TestSplitArgsExpandsEnvironment verifies that $VAR/${VAR} are expanded
// before tokenization, including inside quoted text (restoring the previous
// os.ExpandEnv behavior).
func TestSplitArgsExpandsEnvironment(t *testing.T) {
	t.Setenv("HOME", "/home/testuser")
	t.Setenv("DATA_DIR", "/var/lib/app")

	tests := []struct {
		name string
		cmd  string
		want []string
	}{
		{"dollar var unquoted", `-data-dir=$HOME/data`, []string{"-data-dir=/home/testuser/data"}},
		{"braced var", `--dir ${DATA_DIR}`, []string{"--dir", "/var/lib/app"}},
		{"expansion inside single quotes", `'-data-dir=$HOME/data'`, []string{"-data-dir=/home/testuser/data"}},
		{"expansion inside double quotes", `"-data-dir=$HOME/data"`, []string{"-data-dir=/home/testuser/data"}},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			got := SplitArgs(tt.cmd)
			if !slices.Equal(got, tt.want) {
				t.Errorf("SplitArgs(%q) = %#v, want %#v", tt.cmd, got, tt.want)
			}
		})
	}
}

// TestSplitArgsUnsetVariable verifies that an unset variable expands to the
// empty string (matching os.ExpandEnv): it collapses when unquoted and yields
// an empty quoted argument when inside quotes.
func TestSplitArgsUnsetVariable(t *testing.T) {
	const unset = "AP_MANAGER_TEST_UNSET_VAR_XYZ123"
	t.Setenv(unset, "") // equivalent to unset for os.Getenv

	tests := []struct {
		name string
		cmd  string
		want []string
	}{
		{"unset collapses to nothing", "--x $" + unset, []string{"--x"}},
		{"unset inside quotes yields empty arg", `--name "${` + unset + `}"`, []string{"--name", ""}},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			got := SplitArgs(tt.cmd)
			if !slices.Equal(got, tt.want) {
				t.Errorf("SplitArgs(%q) = %#v, want %#v", tt.cmd, got, tt.want)
			}
		})
	}
}
