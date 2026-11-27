//go:build !windows

package main

import (
	"os"
	"os/user"
	"strconv"
	"syscall"
)

func pathOwner(info os.FileInfo) string {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "unknown"
	}
	uid, gid := strconv.FormatUint(uint64(stat.Uid), 10), strconv.FormatUint(uint64(stat.Gid), 10)
	if account, err := user.LookupId(uid); err == nil {
		uid = account.Username
	}
	if group, err := user.LookupGroupId(gid); err == nil {
		gid = group.Name
	}
	return uid + ":" + gid
}
