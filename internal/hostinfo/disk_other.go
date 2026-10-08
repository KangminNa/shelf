//go:build !unix

package hostinfo

func diskUsage(string) (total, free uint64, ok bool) { return 0, 0, false }
