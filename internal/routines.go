package internal

import (
	"github.com/robfig/cron/v3"

	"github.com/rakibhoossain/go-whatsapp-multidevice-rest/pkg/log"
	pkgWhatsApp "github.com/rakibhoossain/go-whatsapp-multidevice-rest/pkg/whatsapp"
)

func Routines(cron *cron.Cron) {
	log.Print(nil).Info("Running Routine Tasks")

	cron.AddFunc("0 * * * * *", func() {
		// If WhatsAppClient Connection is more than 0
		if len(pkgWhatsApp.WhatsAppActiveTenantClient) > 0 {
			// Check Every Authenticated MSISDN
			for jid, client := range pkgWhatsApp.WhatsAppActiveTenantClient {
				// Get Real JID from Datastore
				realJID := client.Conn.Store.ID.User

				// Mask JID for Logging Information
				maskJID := realJID[0:len(realJID)-4] + "xxxx"

				// Print Log Show Information of Device Checking
				log.Print(nil).Info("Checking WhatsApp Client for " + maskJID)

				// Check WhatsAppClient Registered JID with Authenticated MSISDN
				if jid != realJID {
					// Print Log Show Information to Force Log-out Device
					log.Print(nil).Info("Logging out WhatsApp Client for " + maskJID + " Due to Missmatch Authentication")

					// Logout WhatsAppClient Device
					_ = pkgWhatsApp.WhatsAppLogout(client.User)
					delete(pkgWhatsApp.WhatsAppActiveTenantClient, jid)
				}
			}
		}
	})

	cron.Start()
}
