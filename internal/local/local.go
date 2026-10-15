// Package local reads and writes the cloudflared configuration file
// (~/.cloudflared/config.yml) while preserving the comments and key order a
// human put there.
//
// The whole point of this package is comment preservation, so it works on a
// yaml.v3 node tree and mutates it in place. Decoding into a
// map[string]any would discard every comment, which is exactly what a user
// editing their tunnel config by hand must not lose.
package local

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Rule is one ingress entry. Empty fields are omitted when writing so that a
// bare catch-all renders as a single "service:" line.
type Rule struct {
	Hostname   string
	Path       string
	Service    string
	OriginName string
	HTTPHost   string
}

// File is a cloudflared config.yml document held as a yaml.v3 node tree.
type File struct {
	path string
	root *yaml.Node
}

// backupSuffix is appended to the target path for the pre-write copy.
const backupSuffix = ".bak"

// Load reads path from disk. A missing file is not an error: it yields an
// empty document backed by a mapping node so a later Save can create it.
func Load(path string) (*File, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &File{path: path, root: &yaml.Node{
				Kind:    yaml.DocumentNode,
				Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}},
			}}, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if doc.Kind == 0 || len(doc.Content) == 0 {
		doc = yaml.Node{
			Kind:    yaml.DocumentNode,
			Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}},
		}
	}
	return &File{path: path, root: &doc}, nil
}

// Path reports the file's on-disk location.
func (f *File) Path() string { return f.path }

func (f *File) mapping() *yaml.Node {
	if f.root.Kind == yaml.DocumentNode {
		if len(f.root.Content) == 0 {
			f.root.Content = []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}
		}
		return f.root.Content[0]
	}
	if f.root.Kind == 0 {
		f.root.Kind = yaml.MappingNode
		f.root.Tag = "!!map"
	}
	return f.root
}

func value(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// setValue keeps an existing key in place so comments attached to neighbouring
// entries do not shift when a value is replaced.
func setValue(m *yaml.Node, key string, val *yaml.Node) {
	if m.Kind != yaml.MappingNode {
		m.Kind = yaml.MappingNode
		m.Tag = "!!map"
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1] = val
			return
		}
	}
	m.Content = append(m.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		val,
	)
}

// TunnelNames lists the tunnels in the config: the single-tunnel shape reports
// the configured tunnel name (or "_"), and the multi-tunnel shape reports each
// top-level key in file order.
func (f *File) TunnelNames() []string {
	top := f.mapping()
	if value(top, "ingress") != nil {
		name := "_"
		if n := value(top, "tunnel"); n != nil && n.Value != "" {
			name = n.Value
		}
		return []string{name}
	}
	names := make([]string, 0, len(top.Content)/2)
	for i := 0; i+1 < len(top.Content); i += 2 {
		if top.Content[i+1].Kind == yaml.MappingNode {
			names = append(names, top.Content[i].Value)
		}
	}
	return names
}

// TunnelIngress returns the ingress rules for one tunnel. It accepts both the
// single-tunnel shape (top-level "ingress:") and the multi-tunnel shape where
// the top-level mapping is keyed by tunnel name or UUID.
func (f *File) TunnelIngress(tunnel string) ([]Rule, error) {
	block := f.tunnelBlock(tunnel)
	ing := value(block, "ingress")
	if ing == nil || ing.Kind != yaml.SequenceNode {
		return nil, nil
	}
	rules := make([]Rule, 0, len(ing.Content))
	for _, item := range ing.Content {
		rules = append(rules, decodeRule(item))
	}
	return rules, nil
}

func (f *File) tunnelBlock(tunnel string) *yaml.Node {
	top := f.mapping()
	if value(top, "ingress") != nil {
		return top
	}
	if tunnel != "" {
		if sub := value(top, tunnel); sub != nil && sub.Kind == yaml.MappingNode {
			return sub
		}
	}
	return top
}

func decodeRule(n *yaml.Node) Rule {
	var r Rule
	if n == nil || n.Kind != yaml.MappingNode {
		return r
	}
	r.Hostname = scalar(value(n, "hostname"))
	r.Path = scalar(value(n, "path"))
	r.Service = scalar(value(n, "service"))
	if or := value(n, "originRequest"); or != nil {
		r.OriginName = scalar(value(or, "originName"))
		r.HTTPHost = scalar(value(or, "httpHost"))
	}
	return r
}

func scalar(n *yaml.Node) string {
	if n == nil {
		return ""
	}
	return n.Value
}

// SetTunnelIngress replaces one tunnel's ingress list. Every other key and all
// comments outside that list are left untouched, and a comment attached to an
// ingress entry that survives the edit is carried across. Callers persist with
// Save.
//
// Comments attached to ingress entries are the reason this method is not a
// plain assignment: yaml.v3 stores them on the item nodes, and a rebuild that
// ignores them silently strips a user's notes out of their own config file.
func (f *File) SetTunnelIngress(tunnel string, rules []Rule) error {
	if len(rules) == 0 {
		return fmt.Errorf("set ingress for %q: refusing to write an empty ingress list", tunnel)
	}
	block := f.tunnelBlock(tunnel)

	old := value(block, "ingress")

	// yaml.v3 hoists the head comment of the FIRST sequence item onto the
	// sequence node itself, so the item-level comments only survive if they are
	// carried across explicitly. Build the new list by reusing the old item
	// nodes whose rule is unchanged and re-attaching the stored comments to the
	// rules that are kept.
	carried := commentsFrom(old, rules)

	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	if old != nil {
		seq.HeadComment = old.HeadComment
		seq.LineComment = old.LineComment
		seq.FootComment = old.FootComment
	}
	for i, r := range rules {
		item := encodeRule(r)
		if c, ok := carried[i]; ok {
			attachComments(item, c)
		}
		seq.Content = append(seq.Content, item)
	}

	if old != nil {
		*old = *seq
		return nil
	}
	setValue(block, "ingress", seq)
	return nil
}

// commentSet holds the comment text attached to a decoded ingress item and to
// the values inside it. yaml.v3 keeps a comment written after a value on the
// value's own node, so a line comment such as "service: x # the app" lives on
// the service scalar rather than on the item mapping.
type commentSet struct {
	head, line, foot string
	inner            map[string]commentSet
}

// commentsFrom matches old items to the incoming rules by settled identity
// (hostname+path+service) and returns the comments to re-attach, indexed by the
// new rule position. A rule with no match — a genuinely new entry — gets none.
func commentsFrom(old *yaml.Node, rules []Rule) map[int]commentSet {
	out := map[int]commentSet{}
	if old == nil || old.Kind != yaml.SequenceNode {
		return out
	}
	for i, r := range rules {
		for _, item := range old.Content {
			if item.Kind != yaml.MappingNode {
				continue
			}
			if decodeRule(item) != r {
				continue
			}
			cs := commentSet{item.HeadComment, item.LineComment, item.FootComment, map[string]commentSet{}}
			for j := 0; j+1 < len(item.Content); j += 2 {
				k, v := item.Content[j], item.Content[j+1]
				cs.inner[k.Value] = commentSet{v.HeadComment, v.LineComment, v.FootComment, nil}
			}
			out[i] = cs
			break
		}
	}
	return out
}

// attachComments re-applies a stored commentSet to a freshly encoded item.
func attachComments(item *yaml.Node, cs commentSet) {
	item.HeadComment = cs.head
	item.LineComment = cs.line
	item.FootComment = cs.foot
	for j := 0; j+1 < len(item.Content); j += 2 {
		inner, ok := cs.inner[item.Content[j].Value]
		if !ok {
			continue
		}
		item.Content[j+1].HeadComment = inner.head
		item.Content[j+1].LineComment = inner.line
		item.Content[j+1].FootComment = inner.foot
	}
}

func encodeRule(r Rule) *yaml.Node {
	m := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	add := func(key, val string) {
		if val == "" {
			return
		}
		m.Content = append(m.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: val},
		)
	}
	add("hostname", r.Hostname)
	add("path", r.Path)
	add("service", r.Service)
	if r.OriginName != "" || r.HTTPHost != "" {
		or := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		addTo := func(key, val string) {
			if val == "" {
				return
			}
			or.Content = append(or.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
				&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: val},
			)
		}
		addTo("originName", r.OriginName)
		addTo("httpHost", r.HTTPHost)
		m.Content = append(m.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "originRequest"},
			or,
		)
	}
	return m
}

// Save writes the document back to disk. It is a read-modify-write sequence
// with two safety properties that must not be relaxed:
//
//   - the previous file is copied to path+".bak" before anything is replaced,
//   - the new content lands through a temp file renamed over the target, so a
//     crash mid-write can never leave a truncated config.yml behind.
func (f *File) Save() error {
	dir := filepath.Dir(f.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}

	buf, err := yaml.Marshal(f.root)
	if err != nil {
		return fmt.Errorf("encode %s: %w", f.path, err)
	}

	if _, err := os.Stat(f.path); err == nil {
		if err := f.backup(); err != nil {
			return err
		}
	}

	return writeAtomic(f.path, buf)
}

func (f *File) backup() error {
	src, err := os.Open(f.path)
	if err != nil {
		return fmt.Errorf("backup open %s: %w", f.path, err)
	}
	defer src.Close()

	dst, err := os.OpenFile(f.path+backupSuffix, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("backup create %s%s: %w", f.path, backupSuffix, err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("backup copy %s: %w", f.path, err)
	}
	return dst.Sync()
}

func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".config-*.yml.tmp")
	if err != nil {
		return fmt.Errorf("temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp %s: %w", tmpName, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync temp %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp %s: %w", tmpName, err)
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return fmt.Errorf("chmod temp %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename onto %s: %w", path, err)
	}
	return nil
}
