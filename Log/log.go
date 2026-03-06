package log

import (
	"fmt"
	"runtime"
	"time"
)

func Log(format string, args ...any) {
	pc, _, line, _ := runtime.Caller(1)
	fn := runtime.FuncForPC(pc).Name()
	msg := fmt.Sprintf(format, args...)
	fmt.Printf("[%s] %s:%d  %s\n", time.Now().Format("15:04:05.000"), fn, line, msg)
}
