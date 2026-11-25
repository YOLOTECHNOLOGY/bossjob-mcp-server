package digutil

import "go.uber.org/dig"

// MustGet 从容器中取出指定类型实例，失败时 panic（仅用于启动入口与测试）。
func MustGet[T any](c *dig.Container) T {
	var res T
	if err := c.Invoke(func(t T) { res = t }); err != nil {
		panic(err)
	}
	return res
}
