package auth

import (
	"github.com/rakibhoossain/go-whatsapp-multidevice-rest/pkg/env"
)

var AuthBasicPassword string

func init() {
	AuthBasicPassword, _ = env.GetEnvString("AUTH_BASIC_PASSWORD")
}
