// Command ublk-chown gives an unprivileged ublk device's nodes to the user
// who created it. udev runs it for every new /dev/ublkcN and /dev/ublkbN (see
// 99-ublk-unprivileged.rules); it asks the kernel for the device's owner
// (GET_DEV_INFO) and chowns the node to that uid and gid. Devices created by
// root are left alone.
//
//	ublk-chown ublkc3
package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/ehrlich-b/go-ublk"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: ublk-chown ublkcN|ublkbN")
		os.Exit(2)
	}
	name := os.Args[1]
	idStr := strings.TrimPrefix(strings.TrimPrefix(name, "ublkc"), "ublkb")
	if idStr == name {
		fmt.Fprintf(os.Stderr, "ublk-chown: %s is not a ublk node\n", name)
		os.Exit(2)
	}
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ublk-chown: %s: %v\n", name, err)
		os.Exit(2)
	}
	info, err := ublk.GetDeviceInfo(uint32(id))
	if err != nil {
		fmt.Fprintf(os.Stderr, "ublk-chown: %v\n", err)
		os.Exit(1)
	}
	if !info.Features.Has(ublk.FeatureUnprivileged) {
		return // a privileged device keeps root ownership
	}
	if err := os.Chown("/dev/"+name, int(info.OwnerUID), int(info.OwnerGID)); err != nil {
		fmt.Fprintf(os.Stderr, "ublk-chown: %v\n", err)
		os.Exit(1)
	}
}
