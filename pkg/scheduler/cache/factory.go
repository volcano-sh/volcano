package cache

// bindMethodMap Binder management
var bindMethodMap Binder

// RegisterBindMethod register Bind Method
func RegisterBindMethod(binder Binder) {
	bindMethodMap = binder
}

func GetBindMethod() Binder {
	return bindMethodMap
}

func init() {
	RegisterBindMethod(NewBinder())
}
