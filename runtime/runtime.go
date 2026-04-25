package runtime

import "strings"

// GetSigName strips the "$" prefix from a signal path for use in
// DataStar attributes such as data-bind and data-indicator.
func GetSigName(signalRef string) string {
	return strings.TrimPrefix(signalRef, "$")
}
