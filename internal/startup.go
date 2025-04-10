package internal

import (
	"database/sql"
	"fmt"
	"github.com/rakibhoossain/go-whatsapp-multidevice-rest/pkg/log"
	pkgWhatsApp "github.com/rakibhoossain/go-whatsapp-multidevice-rest/pkg/whatsapp"
	"go.mau.fi/whatsmeow/store"
	"strings"
)

func Startup() {
	log.Print(nil).Info("Running Startup Tasks")

	// Load All WhatsApp Client Devices from Datastore
	devices, err := pkgWhatsApp.WhatsAppDatastore.GetAllDevices()
	if err != nil {
		log.Print(nil).Error("Failed to Load WhatsApp Client Devices from Datastore")
	}

	jidTokenMap := getDeviceTokens(devices)

	// Do Reconnect for Every Device in Datastore
	for _, device := range devices {
		user := jidTokenMap[device.ID.String()]

		if user == nil {
			continue
		}

		// Mask JID for Logging Information
		jid := device.ID.String()
		maskJID := jid[0:len(jid)-4] + "xxxx"

		// Print Restore Log
		log.Print(nil).Info("Restoring WhatsApp Client for " + maskJID)
		log.Print(nil).Info("Restoring WhatsApp Client for UUID " + user.UserToken)

		// Initialize WhatsApp Client
		pkgWhatsApp.WhatsAppInitClient(device, user)

		// Reconnect WhatsApp Client WebSocket
		err = pkgWhatsApp.WhatsAppReconnect(user)
		if err != nil {
			log.Print(nil).Error(err.Error())
		}
	}
}

func getDeviceTokens(devices []*store.Device) map[string]*pkgWhatsApp.WhatsAppTenantUser {
	var jids []string
	for _, device := range devices {
		jids = append(jids, device.ID.String())
	}

	jidTokenMap := make(map[string]*pkgWhatsApp.WhatsAppTenantUser)

	batchSize := 100
	for i := 0; i < len(jids); i += batchSize {
		end := i + batchSize
		if end > len(jids) {
			end = len(jids)
		}
		batch := jids[i:end]

		// Build the PostgreSQL placeholders: $1, $2, ...
		placeholders := make([]string, len(batch))
		args := make([]interface{}, len(batch))
		for i, jid := range batch {
			placeholders[i] = fmt.Sprintf("$%d", i+1)
			args[i] = jid
		}

		query := fmt.Sprintf(`
			SELECT p.jid, p.token, c.webhook_url, c.status_code, c.id AS client_id
			FROM whatsmeow_device_client_pivot p
			INNER JOIN whatsmeow_clients c ON p.client_id = c.id
			WHERE p.jid IN (%s)
			AND c.status_code = 1`, strings.Join(placeholders, ","))

		rows, err := pkgWhatsApp.Db.Query(query, args...)
		if err != nil {
			log.Print(nil).Error("Failed to query pivot table: " + err.Error())
			continue
		}

		func() {
			defer rows.Close()

			for rows.Next() {
				var user pkgWhatsApp.WhatsAppTenantUser
				var jid, webhookURL sql.NullString

				if err := rows.Scan(&jid, &user.UserToken, &webhookURL, &user.StatusCode, &user.ClientId); err != nil {
					log.Print(nil).Error("Failed to scan pivot row: " + err.Error())
					continue
				}

				if jid.Valid {
					user.JID = jid.String
				}
				if webhookURL.Valid {
					user.WebhookURL = webhookURL.String
				}

				jidTokenMap[user.JID] = &user
			}
		}()
	}

	return jidTokenMap
}

// Helper function to convert string slice to interface slice
func convertToInterfaceSlice(strSlice []string) []interface{} {
	interfaceSlice := make([]interface{}, len(strSlice))
	for i, v := range strSlice {
		interfaceSlice[i] = v
	}
	return interfaceSlice
}
