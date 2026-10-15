package local

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Verifier runs a command and returns its combined output. It exists so tests
// can exercise the validate-and-reload pipeline without cloudflared or systemd
// being installed.
type Verifier interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type execVerifier struct{}

func (execVerifier) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// ReloadStrategy names how cloudflared is expected to pick up a new config.
type ReloadStrategy string

const (
	ReloadNone         ReloadStrategy = ""
	ReloadUserSystemd  ReloadStrategy = "systemctl --user restart cloudflared"
	ReloadSystemd      ReloadStrategy = "systemctl restart cloudflared"
	ReloadSendSignal   ReloadStrategy = "cloudflared (send SIGHUP)"
	ReloadLaunchctl    ReloadStrategy = "launchctl kickstart -k cloudflared"
	cloudflaredBinName                = "cloudflared"
)

// ApplyResult reports what the post-write pipeline did. Validation output is
// carried verbatim so the caller can show the user exactly what cloudflared
// said (FR-19).
type ApplyResult struct {
	Validated   bool
	ValidateOut string
	Reloaded    bool
	Strategy    ReloadStrategy
	ReloadNote  string
}

var (
	// ErrValidationFailed means cloudflared rejected the written config. The
	// .bak file still holds the previous working config.
	ErrValidationFailed = errors.New("local: cloudflared rejected the written config")
	// ErrMissingCloudflared means the binary is not on PATH, so neither
	// validation nor reload could run. The write itself succeeded.
	ErrMissingCloudflared = errors.New("local: cloudflared is not installed or not on PATH")
)

// Pipeline writes the document and then runs the post-write steps the PRD
// requires: validate with cloudflared, then reload it if it is running under a
// service manager. Validation failure is reported with the tool's own output.
type Pipeline struct {
	Verifier Verifier
	// LookPath resolves a binary name to a path; nil uses exec.LookPath.
	LookPath func(string) (string, error)
}

// NewPipeline returns a Pipeline using real OS process execution.
func NewPipeline() *Pipeline {
	return &Pipeline{Verifier: execVerifier{}, LookPath: exec.LookPath}
}

func (p *Pipeline) lookPath() func(string) (string, error) {
	if p.LookPath != nil {
		return p.LookPath
	}
	return exec.LookPath
}

func (p *Pipeline) verifier() Verifier {
	if p.Verifier != nil {
		return p.Verifier
	}
	return execVerifier{}
}

// Apply saves f and then validates the result. A failed validation is returned
// as ErrValidationFailed wrapped with cloudflared's verbatim output, so the
// caller can show it rather than a generic message.
func (p *Pipeline) Apply(ctx context.Context, f *File) (*ApplyResult, error) {
	if err := f.Save(); err != nil {
		return nil, err
	}
	res := &ApplyResult{}

	if _, err := p.lookPath()(cloudflaredBinName); err != nil {
		res.ReloadNote = ErrMissingCloudflared.Error()
		return res, nil
	}

	out, err := p.verifier().Run(ctx, cloudflaredBinName, "tunnel", "ingress", "validate", "--config", f.Path())
	res.ValidateOut = string(out)
	if err != nil {
		return res, fmt.Errorf("%w: %s", ErrValidationFailed, res.ValidateOut)
	}
	res.Validated = true

	strategy, found := p.DetectReload()
	if !found {
		res.ReloadNote = "no cloudflared service detected; restart it manually to apply"
		return res, nil
	}
	res.Strategy = strategy

	cmd := reloadCommand(strategy)
	if _, err := p.verifier().Run(ctx, cmd[0], cmd[1:]...); err != nil {
		res.ReloadNote = fmt.Sprintf("reload failed: %v", err)
		return res, nil
	}
	res.Reloaded = true
	return res, nil
}

// DetectReload probes the machine for a running cloudflared service, newest
// integration first, and reports which restart command applies (FR-19).
func (p *Pipeline) DetectReload() (ReloadStrategy, bool) {
	look := p.lookPath()
	v := p.verifier()

	if _, err := look("systemctl"); err == nil {
		if out, err := v.Run(context.Background(), "systemctl", "--user", "is-active", "cloudflared"); err == nil {
			if strings.TrimSpace(string(out)) == "active" {
				return ReloadUserSystemd, true
			}
		}
		if out, err := v.Run(context.Background(), "systemctl", "is-active", "cloudflared"); err == nil {
			if strings.TrimSpace(string(out)) == "active" {
				return ReloadSystemd, true
			}
		}
	}
	if _, err := look("launchctl"); err == nil {
		if _, err := v.Run(context.Background(), "launchctl", "list", "cloudflared"); err == nil {
			return ReloadLaunchctl, true
		}
	}
	if _, err := look(cloudflaredBinName); err == nil {
		return ReloadSendSignal, true
	}
	return ReloadNone, false
}

func reloadCommand(s ReloadStrategy) []string {
	switch s {
	case ReloadUserSystemd:
		return []string{"systemctl", "--user", "restart", cloudflaredBinName}
	case ReloadSystemd:
		return []string{"systemctl", "restart", cloudflaredBinName}
	case ReloadLaunchctl:
		return []string{"launchctl", "kickstart", "-k", "cloudflared"}
	default:
		return []string{cloudflaredBinName, "reload"}
	}
}
