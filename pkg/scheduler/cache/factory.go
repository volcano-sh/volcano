package cache

// bindMethodMap Binder management
var bindMethodMap Binder

// RegisterBindMethod register Bind Method
func RegisterBindMethod(binder Binder) {
	bindMethodMap = binder
}

// GetBindMethod get the registered Binder
func GetBindMethod() Binder {
	return bindMethodMap
}
