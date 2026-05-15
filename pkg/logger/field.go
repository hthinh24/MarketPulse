package logger

import (
	"go.uber.org/zap"
	"time"
)

func String(key, val string) Field  { return zap.String(key, val) }
func Int(key string, val int) Field { return zap.Int(key, val) }
func Uint32(key string, val uint32) Field {
	return zap.Uint32(key, val)
}
func Uint64(key string, val uint64) Field {
	return zap.Uint64(key, val)
}
func Int64(key string, val int64) Field {
	return zap.Int64(key, val)
}
func Bool(key string, val bool) Field {
	return zap.Bool(key, val)
}
func Duration(key string, val time.Duration) Field {
	return zap.Duration(key, val)
}
func Error(err error) Field { return zap.Error(err) }
