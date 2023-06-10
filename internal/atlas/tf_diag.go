package atlas

import (
	"github.com/hashicorp/terraform-plugin-framework/diag"
)

// ErrorDiagnostic checks if the given error is a diagnostic.
// If it is, it returns the diagnostic error.
// Otherwise, it returns a new diagnostic error with the given error as the detail.
func ErrorDiagnostic(err error, summary string) diag.Diagnostic {
	if diag, ok := err.(diag.Diagnostic); ok {
		return diag
	}
	return diag.NewErrorDiagnostic(summary, err.Error())
}

// Severity implements the diag.Diagnostic interface.
func (e cliError) Severity() diag.Severity {
	return diag.SeverityError
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
