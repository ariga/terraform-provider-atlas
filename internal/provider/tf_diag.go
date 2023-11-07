package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/diag"
)

// errorDiagnostic checks if the given error is a diagnostic.
// If it is, it returns the diagnostic error.
// Otherwise, it returns a new diagnostic error with the given error as the detail.
func errorDiagnostic(err error, summary string) diag.Diagnostic {
	type diagError interface {
		Summary() string
		Detail() string
	}
	if d, ok := err.(diagError); ok {
		return diag.NewErrorDiagnostic(d.Summary(), d.Detail())
	}
	return diag.NewErrorDiagnostic(summary, err.Error())
}
