//go:build !darwin

package ese

func cloneFile(src, dst string) error { return copyFile(src, dst) }
