package internal

import (
	"database/sql"
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

		jid := pkgWhatsApp.WhatsAppDecomposeJID(device.ID.User)
		token := jidTokenMap[jid]

		if token == "" {
			continue
		}

		// Mask JID for Logging Information
		maskJID := jid[0:len(jid)-4] + "xxxx"

		// Print Restore Log
		log.Print(nil).Info("Restoring WhatsApp Client for " + maskJID)
		log.Print(nil).Info("Restoring WhatsApp Client for UUID " + token)

		// Initialize WhatsApp Client
		pkgWhatsApp.WhatsAppInitClient(device, token)

		// Reconnect WhatsApp Client WebSocket
		err = pkgWhatsApp.WhatsAppReconnect(token)
		if err != nil {
			log.Print(nil).Error(err.Error())
		}
	}
}

func getDeviceTokens(devices []*store.Device) map[string]string {
	// Extract all JIDs first
	var jids []string
	for _, device := range devices {
		jids = append(jids, pkgWhatsApp.WhatsAppDecomposeJID(device.ID.User))
	}

	var jidTokenMap = make(map[string]string)

	// Process in chunks of 100
	batchSize := 100
	for i := 0; i < len(jids); i += batchSize {
		end := i + batchSize
		if end > len(jids) {
			end = len(jids)
		}
		batch := jids[i:end]

		// Query pivot table for this batch
		rows, err := pkgWhatsApp.Db.Query(`
			SELECT p.jid, p.token 
    		FROM whatsmeow_device_client_pivot p
    		INNER JOIN whatsmeow_clients c ON p.client_id = c.id
    		WHERE p.jid IN (`+strings.Repeat("?,", len(batch)-1)+`?)
    		AND c.status_code = 1`,
			convertToInterfaceSlice(batch)...,
		)
		if err != nil {
			log.Print(nil).Error("Failed to query pivot table: " + err.Error())
			continue
		}

		defer func(rows *sql.Rows) {
			err := rows.Close()
			if err != nil {

			}
		}(rows)

		for rows.Next() {
			var jid, token string
			if err := rows.Scan(&jid, &token); err != nil {
				log.Print(nil).Error("Failed to scan pivot row: " + err.Error())
				continue
			}
			jidTokenMap[jid] = token
		}
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
