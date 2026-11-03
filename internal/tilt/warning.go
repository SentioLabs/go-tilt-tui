package tilt

// HasWarning reports whether the latest build finished with warnings and
// without an error. It is a flag beside the status, not a status: a
// healthy resource can also have warnings. Combined() ignores warnings.
func (r Resource) HasWarning() bool {
	if len(r.Status.BuildHistory) == 0 {
		return false
	}
	b := r.Status.BuildHistory[0]
	return len(b.Warnings) > 0 && b.Error == ""
}
