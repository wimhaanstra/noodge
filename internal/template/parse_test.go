package template

import "testing"

func TestParse(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		want  []Ref
		isErr bool
	}{
		{
			name: "no placeholders",
			in:   "npm run build",
		},
		{
			name: "bare value",
			in:   "node app.js --host {{host}}",
			want: []Ref{{Kind: KindValue, Name: "host"}},
		},
		{
			name: "flag form",
			in:   "node app.js {{flag host}}",
			want: []Ref{{Kind: KindFlag, Name: "host"}},
		},
		{
			name: "several, in order",
			in:   "node app.js {{flag host}} {{flag cert}} {{verbose}}",
			want: []Ref{
				{Kind: KindFlag, Name: "host"},
				{Kind: KindFlag, Name: "cert"},
				{Kind: KindValue, Name: "verbose"},
			},
		},
		{
			name: "args",
			in:   "go test ./... {{args}}",
			want: []Ref{{Kind: KindArgs}},
		},
		{
			name: "inner whitespace is tolerated",
			in:   "x {{  flag   host  }}",
			want: []Ref{{Kind: KindFlag, Name: "host"}},
		},
		{
			name: "names may contain digits, dashes and underscores",
			in:   "x {{my-param_2}}",
			want: []Ref{{Kind: KindValue, Name: "my-param_2"}},
		},
		{name: "unclosed", in: "x {{host", isErr: true},
		{name: "empty", in: "x {{}}", isErr: true},
		{name: "flag with no name", in: "x {{flag}}", isErr: true},
		{name: "name with a space", in: "x {{my param}}", isErr: true},
		{name: "name starting with a digit", in: "x {{2fast}}", isErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.in)

			if tt.isErr {
				if err == nil {
					t.Fatalf("Parse(%q): expected an error, got %v", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse(%q): %v", tt.in, err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %d refs %v, want %d %v", len(got), got, len(tt.want), tt.want)
			}
			for i := range tt.want {
				if got[i].Kind != tt.want[i].Kind || got[i].Name != tt.want[i].Name {
					t.Errorf("ref %d: got {%v %q}, want {%v %q}",
						i, got[i].Kind, got[i].Name, tt.want[i].Kind, tt.want[i].Name)
				}
			}
		})
	}
}

// The offsets have to be exact, because expansion replaces by slice bounds.
func TestParseOffsets(t *testing.T) {
	in := "node app.js {{flag host}} end"

	refs, err := Parse(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 {
		t.Fatalf("got %d refs, want 1", len(refs))
	}

	if got := in[refs[0].Start:refs[0].End]; got != "{{flag host}}" {
		t.Errorf("bounds cover %q, want {{flag host}}", got)
	}
}

func TestValidName(t *testing.T) {
	valid := []string{"host", "_x", "a1", "my-param", "my_param"}
	invalid := []string{"", "1abc", "my param", "a.b", "a:b", "--host"}

	for _, s := range valid {
		if !ValidName(s) {
			t.Errorf("ValidName(%q) = false, want true", s)
		}
	}
	for _, s := range invalid {
		if ValidName(s) {
			t.Errorf("ValidName(%q) = true, want false", s)
		}
	}
}
