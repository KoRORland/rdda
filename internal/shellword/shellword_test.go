package shellword

import (
	"reflect"
	"testing"
)

func TestSplit(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"   ", nil},
		{"sudo systemctl reload-or-restart rdda-singbox",
			[]string{"sudo", "systemctl", "reload-or-restart", "rdda-singbox"}},
		{"  extra   spaces\tand\ttabs ", []string{"extra", "spaces", "and", "tabs"}},
		// The failure strings.Fields could not handle: a spaced path.
		{`/opt/my tools/reload.sh`, []string{"/opt/my", "tools/reload.sh"}},
		{`"/opt/my tools/reload.sh" --now`, []string{"/opt/my tools/reload.sh", "--now"}},
		{`'/opt/my tools/reload.sh'`, []string{"/opt/my tools/reload.sh"}},
		{`a\ b c`, []string{"a b", "c"}},
		{`say "hello world"`, []string{"say", "hello world"}},
		{`mix'ed'"quotes"`, []string{"mixedquotes"}},
		{`empty "" arg`, []string{"empty", "", "arg"}},
		{`esc "a\"b"`, []string{"esc", `a"b`}},
	}
	for _, c := range cases {
		got, err := Split(c.in)
		if err != nil {
			t.Fatalf("Split(%q) error: %v", c.in, err)
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Fatalf("Split(%q) = %#v, want %#v", c.in, got, c.want)
		}
	}
}

func TestSplit_Errors(t *testing.T) {
	for _, in := range []string{`"unterminated`, `'unterminated`, `trailing\`} {
		if _, err := Split(in); err == nil {
			t.Fatalf("Split(%q) should error", in)
		}
	}
}
