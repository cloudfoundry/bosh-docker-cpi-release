package vm

import (
	"strings"
)

type ipAddr struct {
	ip string
}

func newIPAddr(ip string) ipAddr { return ipAddr{ip} }

func (a ipAddr) IsV6() bool { return strings.Contains(a.ip, ":") }
