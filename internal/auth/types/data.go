package types

import pkgWhatsApp "github.com/rakibhoossain/go-whatsapp-multidevice-rest/pkg/whatsapp"

type AuthBasicPayload struct {
	User *pkgWhatsApp.WhatsAppTenantUser
}
