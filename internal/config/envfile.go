package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/jorden-parker/local-coding-agent-setup/internal/atomicfile"
)

// assignment matches KEY=VALUE lines, with an optional export prefix.
var assignment = regexp.MustCompile(`^\s*(?:export\s+)?([A-Z_][A-Z0-9_]*)=(.*)$`)

// bare values need no quoting in bash.
var bare = regexp.MustCompile(`^[A-Za-z0-9_./:+,=-]*$`)

// File is a config.env file kept line by line so Save reproduces comments,
// blank lines, ordering and unknown keys byte for byte.
type File struct {
	Path   string
	Exists bool
	lines  []string
	eol    bool // original file ended with a newline
}

// Load reads path. A missing file yields an empty File with Exists false.
func Load(path string) (*File, error) {
	f := &File{Path: path, eol: true}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return f, nil
	}
	if err != nil {
		return nil, err
	}
	f.Exists = true
	s := string(b)
	if s == "" {
		f.lines = nil
		return f, nil
	}
	f.eol = strings.HasSuffix(s, "\n")
	s = strings.TrimSuffix(s, "\n")
	f.lines = strings.Split(s, "\n")
	return f, nil
}

// Get returns the value of key (last assignment wins, as in bash).
func (f *File) Get(key string) (string, bool) {
	val, found := "", false
	for _, l := range f.lines {
		if m := assignment.FindStringSubmatch(l); m != nil && m[1] == key {
			val, found = unquote(m[2]), true
		}
	}
	return val, found
}

// Values returns every assignment in the file.
func (f *File) Values() map[string]string {
	out := map[string]string{}
	for _, l := range f.lines {
		if m := assignment.FindStringSubmatch(l); m != nil {
			out[m[1]] = unquote(m[2])
		}
	}
	return out
}

// Set rewrites the first assignment of key in place, keeping an export
// prefix, or appends a new line.
func (f *File) Set(key, value string) {
	for i, l := range f.lines {
		m := assignment.FindStringSubmatch(l)
		if m == nil || m[1] != key {
			continue
		}
		prefix := l[:strings.Index(l, key)]
		f.lines[i] = prefix + key + "=" + quote(value)
		return
	}
	f.lines = append(f.lines, key+"="+quote(value))
	f.eol = true
}

// Bytes renders the file.
func (f *File) Bytes() []byte {
	if len(f.lines) == 0 {
		return nil
	}
	s := strings.Join(f.lines, "\n")
	if f.eol {
		s += "\n"
	}
	return []byte(s)
}

// Save writes the file atomically.
func (f *File) Save() error {
	if err := atomicfile.Write(f.Path, f.Bytes()); err != nil {
		return fmt.Errorf("write %s: %w", f.Path, err)
	}
	f.Exists = true
	return nil
}

// quote renders a value for a bash assignment.
func quote(v string) string {
	if bare.MatchString(v) {
		return v
	}
	return "'" + strings.ReplaceAll(v, "'", `'\''`) + "'"
}

// unquote parses the right-hand side of a bash assignment: a bare word, a
// single-quoted string, or a double-quoted string. No $ expansion. A trailing
// unquoted comment is dropped.
func unquote(raw string) string {
	raw = strings.TrimSpace(raw)
	var b strings.Builder
	for i := 0; i < len(raw); {
		c := raw[i]
		switch {
		case c == '\'':
			j := strings.IndexByte(raw[i+1:], '\'')
			if j < 0 {
				b.WriteString(raw[i+1:])
				return b.String()
			}
			b.WriteString(raw[i+1 : i+1+j])
			i += j + 2
		case c == '"':
			i++
			for i < len(raw) && raw[i] != '"' {
				if raw[i] == '\\' && i+1 < len(raw) && strings.IndexByte(`"\$`+"`", raw[i+1]) >= 0 {
					i++
				}
				b.WriteByte(raw[i])
				i++
			}
			i++
		case c == '#':
			if b.Len() == 0 || i == 0 || raw[i-1] == ' ' || raw[i-1] == '\t' {
				return strings.TrimRight(b.String(), " \t")
			}
			b.WriteByte(c)
			i++
		case c == '\\' && i+1 < len(raw):
			b.WriteByte(raw[i+1])
			i += 2
		case c == ' ' || c == '\t':
			// unquoted whitespace ends the word unless more quoted text follows
			rest := strings.TrimLeft(raw[i:], " \t")
			if rest == "" || rest[0] == '#' {
				return b.String()
			}
			b.WriteByte(c)
			i++
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}
