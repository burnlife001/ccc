// ccc —— Go 单 exe 版 Claude CLI 切换器。
// 入口唯一:解析 os.Args → Dispatch → 处理错误退出。
//
// 架构约束见 ./go架构规范.md。
package main

import (
	"fmt"
	"os"
)

func main() {
	// panic 兜底:任何业务路径 panic 都打印堆栈并 exit 2,
	// 避免给用户一个静默的 crash。
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "ccc: internal panic: %v\n", r)
			os.Exit(2)
		}
	}()

	loadDotEnv() // <exeDir>/.env → 进程环境(已有环境变量优先)

	code := Dispatch(os.Args[1:])
	os.Exit(code)
}
