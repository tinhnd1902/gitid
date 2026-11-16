package config_test

import "reflect"

func reflectEqual(a, b any) bool { return reflect.DeepEqual(a, b) }
