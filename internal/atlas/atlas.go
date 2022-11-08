package atlas

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"runtime"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

type (
	// Client is a client for the Atlas CLI.
	Client struct {
		path string
	}
	// ApplyParams are the parameters for the `migrate apply` command.
	ApplyParams struct {
		DirURL          string
		URL             string
		RevisionsSchema string
		BaselineVersion string
		TxMode          string
		Amount          uint
	}
	// StatusParams are the parameters for the `migrate status` command.
	StatusParams struct {
		DirURL          string
		URL             string
		RevisionsSchema string
	}
)

// NewClient returns a new Atlas client.
// The client will try to find the Atlas CLI in the current directory,
// and in the PATH.
func NewClient(ctx context.Context, dir, name string) (*Client, error) {
	path, err := execPath(ctx, dir, name)
	if err != nil {
		return nil, err
	}
	return NewClientWithPath(path), nil
}

// NewClientWithPath returns a new Atlas client with the given atlas-cli path.
func NewClientWithPath(path string) *Client {
	return &Client{path: path}
}

// Apply runs the `migrate apply` command.
func (c *Client) Apply(ctx context.Context, data *ApplyParams) (*ApplyReport, error) {
	args := []string{
		"migrate", "apply", "--log", "{{ json . }}",
		"--url", data.URL,
		"--dir", fmt.Sprintf("file://%s", data.DirURL),
	}
	if data.RevisionsSchema != "" {
		args = append(args, "--revisions-schema", data.RevisionsSchema)
	}
	if data.BaselineVersion != "" {
		args = append(args, "--baseline", data.BaselineVersion)
	}
	if data.TxMode != "" {
		args = append(args, "--tx-mode", data.TxMode)
	}
	if data.Amount > 0 {
		args = append(args, fmt.Sprintf("%d", data.Amount))
	}
	var report ApplyReport
	if err := c.runCommand(ctx, args, &report); err != nil {
		return nil, err
	}
	return &report, nil
}

// Status runs the `migrate status` command.
func (c *Client) Status(ctx context.Context, data *StatusParams) (*StatusReport, error) {
	args := []string{
		"migrate", "status", "--log", "{{ json . }}",
		"--url", data.URL,
		"--dir", fmt.Sprintf("file://%s", data.DirURL),
	}
	if data.RevisionsSchema != "" {
		args = append(args, "--revisions-schema", data.RevisionsSchema)
	}
	var report StatusReport
	if err := c.runCommand(ctx, args, &report); err != nil {
		return nil, err
	}
	return &report, nil
}

// runCommand runs the given command and unmarshals the output into the given
// interface.
func (c *Client) runCommand(ctx context.Context, args []string, report interface{}) error {
	cmd := exec.CommandContext(ctx, c.path, args...)
	cmd.Env = append(cmd.Env, "ATLAS_NO_UPDATE_NOTIFIER=1")
	output, err := cmd.Output()
	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			return err
		}
		if exitErr.Stderr != nil && len(exitErr.Stderr) > 0 {
			return &cliError{
				summary: string(exitErr.Stderr),
				detail:  string(output),
			}
		}
		if exitErr.ExitCode() != 1 || !json.Valid(output) {
			return &cliError{
				summary: "Atlas CLI",
				detail:  string(output),
			}
		}
		// When the exit code is 1, it means that the command
		// was executed successfully, and the output is a JSON
	}
	if err := json.Unmarshal(output, report); err != nil {
		return fmt.Errorf("atlas: unable to decode the report %w", err)
	}
	return nil
}

// LatestVersion returns the latest version of the migrations directory.
func (r StatusReport) LatestVersion() string {
	if l := len(r.Available); l > 0 {
		return r.Available[l-1].Version
	}
	return ""
}

// Amount returns the number of migrations need to apply
// for the given version.
//
// The second return value is true if the version is found
// and the database is up-to-date.
//
// If the version is not found, it returns 0 and the second
// return value is false.
func (r StatusReport) Amount(version string) (amount uint, ok bool) {
	if version == "" {
		amount := uint(len(r.Pending))
		return amount, amount == 0
	}
	if r.Current == version {
		return amount, true
	}
	for idx, v := range r.Pending {
		if v.Version == version {
			amount = uint(idx + 1)
			break
		}
	}
	return amount, false
}

func execPath(ctx context.Context, dir, name string) (string, error) {
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	tflog.Debug(ctx, "atlas: looking for the Atlas CLI in the current directory", map[string]interface{}{
		"dir":  dir,
		"path": path.Join(dir, name),
		"name": name,
	})
	p := path.Join(dir, name)
	if _, err := os.Stat(p); os.IsExist(err) {
		return p, nil
	}
	tflog.Debug(ctx, "atlas: looking for the Atlas CLI in the $PATH", map[string]interface{}{
		"name": name,
	})
	// If the binary is not in the current directory,
	// try to find it in the PATH.
	return exec.LookPath(name)
}

type cliError struct {
	summary string
	detail  string
}

// Error implements the error interface.
func (e cliError) Error() string {
	return e.summary
}

// Severity implements the diag.Diagnostic interface.
func (e cliError) Severity() diag.Severity {
	return diag.SeverityError
}

// Summary implements the diag.Diagnostic interface.
func (e cliError) Summary() string {
	if strings.HasPrefix(e.summary, "Error: ") {
		return e.summary[7:]
	}
	return e.summary
}

// Detail implements the diag.Diagnostic interface.
func (e cliError) Detail() string {
	return strings.TrimSpace(e.detail)
}

// Equal implements the diag.Diagnostic interface.
func (e cliError) Equal(other diag.Diagnostic) bool {
	if other == nil {
		return false
	}
	if o, ok := other.(*cliError); ok && o != nil {
		return e.summary == o.summary && e.detail == o.detail
	}
	return false
}

// ErrorDiagnostic checks if the given error is a diagnostic.
// If it is, it returns the diagnostic error.
// Otherwise, it returns a new diagnostic error with the given error as the detail.
func ErrorDiagnostic(err error, summary string) diag.Diagnostic {
	if diag, ok := err.(diag.Diagnostic); ok {
		return diag
	}
	return diag.NewErrorDiagnostic(summary, err.Error())
}
