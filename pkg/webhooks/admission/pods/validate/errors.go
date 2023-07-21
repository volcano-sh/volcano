package validate

// MultipleGPUShareError is MultipleGPUShareError
type MultipleGPUShareError struct {
	failReason string
}

func (mge *MultipleGPUShareError) Error() string {
	return mge.failReason
}

// NewMultipleGPUShareError is NewMultipleGPUShareError
func NewMultipleGPUShareError(f string) error {
	return &MultipleGPUShareError{failReason: f}
}

// NotExistMultipleGPUAnno is NotExistMultipleGPUAnno
func NotExistMultipleGPUAnno(err error) bool {
	if gsErr, ok := err.(*MultipleGPUShareError); ok {
		return gsErr.failReason == "container-multiple-gpu hasn't been set"
	}
	return false
}
