package whatsapp

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/labstack/echo/v4"
	"github.com/rakibhoossain/go-whatsapp-multidevice-rest/pkg/router"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"io"
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/forPelevin/gomoji"
	webp "github.com/nickalie/go-webpbin"
	"github.com/rivo/uniseg"
	"github.com/sunshineplan/imgconv"

	qrCode "github.com/skip2/go-qrcode"
	"google.golang.org/protobuf/proto"

	"github.com/rakibhoossain/go-whatsapp-multidevice-rest/pkg/env"
	"github.com/rakibhoossain/go-whatsapp-multidevice-rest/pkg/log"
	"go.mau.fi/whatsmeow"
	wabin "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waCompanionReg"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
)

type WhatsAppTenantUser struct {
	JID        string `json:"jid"`
	UserToken  string `json:"token"`
	WebhookURL string `json:"webhook_url"`
	ClientId   int64  `json:"client_id"`
	StatusCode int    `json:"status_code"`
}

type WhatsAppTenantClient struct {
	Conn *whatsmeow.Client   // Explicitly named connection
	User *WhatsAppTenantUser // User data
}

var WhatsAppDatastore *sqlstore.Container
var WhatsAppActiveTenantClient = make(map[string]*WhatsAppTenantClient)

var Db *sql.DB

var (
	WhatsAppClientProxyURL string
)

func init() {
	var err error

	dbType, err := env.GetEnvString("WHATSAPP_DATASTORE_TYPE")
	if err != nil {
		log.Print(nil).Fatal("Error Parse Environment Variable for WhatsApp Client Datastore Type")
	}

	dbURI, err := env.GetEnvString("WHATSAPP_DATASTORE_URI")
	if err != nil {
		log.Print(nil).Fatal("Error Parse Environment Variable for WhatsApp Client Datastore URI")
	}

	datastore, err := sqlstore.New(dbType, dbURI, nil)
	if err != nil {
		log.Print(nil).Fatal(err)
		log.Print(nil).Fatal("Error Connect WhatsApp Client Datastore")
	}

	WhatsAppClientProxyURL, _ = env.GetEnvString("WHATSAPP_CLIENT_PROXY_URL")

	WhatsAppDatastore = datastore
	Db = datastore.GetDB()
}

func WhatsAppInitClient(device *store.Device, user *WhatsAppTenantUser) {
	var err error
	wabin.IndentXML = true

	if WhatsAppActiveTenantClient[user.UserToken] == nil {
		if device == nil {
			// Initialize New WhatsApp Client Device in Datastore
			device = WhatsAppDatastore.NewDevice()
		}

		// Set Client Properties
		store.DeviceProps.Os = proto.String(WhatsAppGetUserOS())
		store.DeviceProps.PlatformType = WhatsAppGetUserAgent("chrome").Enum()
		store.DeviceProps.RequireFullSync = proto.Bool(false)

		// Set Client Versions
		version.Major, err = env.GetEnvInt("WHATSAPP_VERSION_MAJOR")
		if err == nil {
			store.DeviceProps.Version.Primary = proto.Uint32(uint32(version.Major))
		}
		version.Minor, err = env.GetEnvInt("WHATSAPP_VERSION_MINOR")
		if err == nil {
			store.DeviceProps.Version.Secondary = proto.Uint32(uint32(version.Minor))
		}
		version.Patch, err = env.GetEnvInt("WHATSAPP_VERSION_PATCH")
		if err == nil {
			store.DeviceProps.Version.Tertiary = proto.Uint32(uint32(version.Patch))
		}

		// Initialize New WhatsApp Client
		// And Save it to The Map
		var wc WhatsAppTenantClient
		wc.Conn = whatsmeow.NewClient(device, waLog.Noop)
		wc.User = user

		WhatsAppActiveTenantClient[user.UserToken] = &wc
		WhatsAppActiveTenantClient[user.UserToken].Conn.AddEventHandler(createEventHandler(user))

		// Set WhatsApp Client Proxy Address if Proxy URL is Provided
		if len(WhatsAppClientProxyURL) > 0 {
			WhatsAppActiveTenantClient[user.UserToken].Conn.SetProxyAddress(WhatsAppClientProxyURL)
		}

		// Set WhatsApp Client Auto Reconnect
		WhatsAppActiveTenantClient[user.UserToken].Conn.EnableAutoReconnect = true

		// Set WhatsApp Client Auto Trust Identity
		WhatsAppActiveTenantClient[user.UserToken].Conn.AutoTrustIdentity = true
	}
}

func createEventHandler(user *WhatsAppTenantUser) func(interface{}) {
	return func(evt interface{}) {
		switch v := evt.(type) {
		case *events.PairSuccess:
			handlePairedEvent(user, v)
		case *events.LoggedOut:
			handleLoggedOutEvent(user)
		}
	}
}

func handlePairedEvent(user *WhatsAppTenantUser, evt *events.PairSuccess) {
	err := saveUUID(evt.ID, user)
	if err != nil {
		log.Print(nil).Info("Store JID failed: " + user.UserToken)
		return
	}

	sendWebhookEvent("PAIR_SUCCESS", *user)
}

func handleLoggedOutEvent(user *WhatsAppTenantUser) {

	tmpUser := *user

	log.Print(nil).Info("logout UUID: " + user.UserToken)

	if WhatsAppActiveTenantClient[user.UserToken] != nil {
		err := WhatsAppLogout(user)
		if err != nil {
		}
		if WhatsAppActiveTenantClient[user.UserToken] != nil {
			delete(WhatsAppActiveTenantClient, user.UserToken)
		}
	}

	err := removeByUUID(user)
	if err != nil {
		log.Print(nil).Info("logout db failed UUID remove: " + err.Error())
	}

	sendWebhookEvent("LOGGED_OUT", tmpUser)
}

func WhatsAppGetUserAgent(agentType string) waCompanionReg.DeviceProps_PlatformType {
	switch strings.ToLower(agentType) {
	case "desktop":
		return waCompanionReg.DeviceProps_DESKTOP
	case "mac":
		return waCompanionReg.DeviceProps_CATALINA
	case "android":
		return waCompanionReg.DeviceProps_ANDROID_AMBIGUOUS
	case "android-phone":
		return waCompanionReg.DeviceProps_ANDROID_PHONE
	case "andorid-tablet":
		return waCompanionReg.DeviceProps_ANDROID_TABLET
	case "ios-phone":
		return waCompanionReg.DeviceProps_IOS_PHONE
	case "ios-catalyst":
		return waCompanionReg.DeviceProps_IOS_CATALYST
	case "ipad":
		return waCompanionReg.DeviceProps_IPAD
	case "wearos":
		return waCompanionReg.DeviceProps_WEAR_OS
	case "ie":
		return waCompanionReg.DeviceProps_IE
	case "edge":
		return waCompanionReg.DeviceProps_EDGE
	case "chrome":
		return waCompanionReg.DeviceProps_CHROME
	case "safari":
		return waCompanionReg.DeviceProps_SAFARI
	case "firefox":
		return waCompanionReg.DeviceProps_FIREFOX
	case "opera":
		return waCompanionReg.DeviceProps_OPERA
	case "uwp":
		return waCompanionReg.DeviceProps_UWP
	case "aloha":
		return waCompanionReg.DeviceProps_ALOHA
	case "tv-tcl":
		return waCompanionReg.DeviceProps_TCL_TV
	default:
		return waCompanionReg.DeviceProps_UNKNOWN
	}
}

func WhatsAppGetUserOS() string {
	switch runtime.GOOS {
	case "windows":
		return "Windows"
	case "darwin":
		return "macOS"
	default:
		return "Linux"
	}
}

func WhatsAppGenerateQR(qrChan <-chan whatsmeow.QRChannelItem) (string, int) {
	qrChanCode := make(chan string)
	qrChanTimeout := make(chan int)

	// Get QR Code Data and Timeout
	go func() {
		for evt := range qrChan {
			if evt.Event == "code" {
				qrChanCode <- evt.Code
				qrChanTimeout <- int(evt.Timeout.Seconds())
			}
		}
	}()

	// Generate QR Code Data to PNG Image
	qrTemp := <-qrChanCode
	qrPNG, _ := qrCode.Encode(qrTemp, qrCode.Medium, 256)

	// Return QR Code PNG in Base64 Format and Timeout Information
	return base64.StdEncoding.EncodeToString(qrPNG), <-qrChanTimeout
}

func WhatsAppLogin(user *WhatsAppTenantUser) (string, int, error) {
	if WhatsAppActiveTenantClient[user.UserToken] != nil {
		// Make Sure WebSocket Connection is Disconnected
		WhatsAppActiveTenantClient[user.UserToken].Conn.Disconnect()

		if WhatsAppActiveTenantClient[user.UserToken].Conn.Store.ID == nil {

			// Clean history
			delete(WhatsAppActiveTenantClient, user.UserToken)
			WhatsAppInitClient(nil, user)

			if WhatsAppActiveTenantClient[user.UserToken] != nil {

				// Device ID is not Exist
				// Generate QR Code
				qrChanGenerate, _ := WhatsAppActiveTenantClient[user.UserToken].Conn.GetQRChannel(context.Background())

				// Connect WebSocket while Initialize QR Code Data to be Sent
				err := WhatsAppActiveTenantClient[user.UserToken].Conn.Connect()
				if err != nil {
					return "", 0, err
				}

				// Get Generated QR Code and Timeout Information
				qrImage, qrTimeout := WhatsAppGenerateQR(qrChanGenerate)

				// Return QR Code in Base64 Format and Timeout Information
				return "data:image/png;base64," + qrImage, qrTimeout, nil
			}

			return "", 0, errors.New("Please try again")

		} else {
			// Device ID is Exist
			// Reconnect WebSocket
			err := WhatsAppReconnect(user)
			if err != nil {
				return "", 0, err
			}

			return "WhatsApp Client is Reconnected", 0, nil
		}
	}

	// Return Error WhatsApp Client is not Valid
	return "", 0, errors.New("WhatsAppLogin WhatsApp Client is not Valid")
}

func WhatsAppStatusCheck(user *WhatsAppTenantUser) (string, error) {
	if WhatsAppActiveTenantClient[user.UserToken] != nil {
		if WhatsAppActiveTenantClient[user.UserToken].Conn.Store.ID != nil {
			return "Connected", nil
		}
	}

	// Return Error WhatsApp Client is not Valid
	return "", errors.New("Not connected")
}

func WhatsAppLoginPair(user *WhatsAppTenantUser) (string, int, error) {
	if WhatsAppActiveTenantClient[user.UserToken] != nil {
		// Make Sure WebSocket Connection is Disconnected
		WhatsAppActiveTenantClient[user.UserToken].Conn.Disconnect()

		if WhatsAppActiveTenantClient[user.UserToken].Conn.Store.ID == nil {
			// Connect WebSocket while also Requesting Pairing Code
			err := WhatsAppActiveTenantClient[user.UserToken].Conn.Connect()
			if err != nil {
				return "", 0, err
			}

			jid := WhatsAppDecomposeJID(user.JID)
			// Request Pairing Code
			code, err := WhatsAppActiveTenantClient[user.UserToken].Conn.PairPhone(jid, true, whatsmeow.PairClientChrome, "Chrome ("+WhatsAppGetUserOS()+")")
			if err != nil {
				return "", 0, err
			}

			return code, 160, nil
		} else {
			// Device ID is Exist
			// Reconnect WebSocket
			err := WhatsAppReconnect(user)
			if err != nil {
				return "", 0, err
			}

			return "WhatsApp Client is Reconnected", 0, nil
		}
	}

	// Return Error WhatsApp Client is not Valid
	return "", 0, errors.New("WhatsAppLoginPair WhatsApp Client is not Valid")
}

func WhatsAppReconnect(user *WhatsAppTenantUser) error {
	if WhatsAppActiveTenantClient[user.UserToken] != nil {
		// Make Sure WebSocket Connection is Disconnected
		WhatsAppActiveTenantClient[user.UserToken].Conn.Disconnect()

		// Make Sure Store ID is not Empty
		// To do Reconnection
		if WhatsAppActiveTenantClient[user.UserToken] != nil {
			err := WhatsAppActiveTenantClient[user.UserToken].Conn.Connect()
			if err != nil {
				return err
			}

			return nil
		}

		return errors.New("WhatsApp Client Store ID is Empty, Please Re-Login and Scan QR Code Again")
	}

	return errors.New("WhatsAppReconnect WhatsApp Client is not Valid")
}

func WhatsAppLogout(user *WhatsAppTenantUser) error {
	if WhatsAppActiveTenantClient[user.UserToken] != nil {
		// Make Sure Store ID is not Empty
		if WhatsAppActiveTenantClient[user.UserToken] != nil {
			var err error

			// Set WhatsApp Client Presence to Unavailable
			WhatsAppPresence(user, false)

			// Logout WhatsApp Client and Disconnect from WebSocket
			err = WhatsAppActiveTenantClient[user.UserToken].Conn.Logout()
			if err != nil {
				// Force Disconnect
				WhatsAppActiveTenantClient[user.UserToken].Conn.Disconnect()

				// Manually Delete Device from Datastore Store
				err = WhatsAppActiveTenantClient[user.UserToken].Conn.Store.Delete()
				if err != nil {
					return err
				}
			}

			// Free WhatsApp Client Map
			WhatsAppActiveTenantClient[user.UserToken] = nil
			delete(WhatsAppActiveTenantClient, user.UserToken)

			return nil
		}

		return errors.New("WhatsApp Client Store ID is Empty, Please Re-Login and Scan QR Code Again")
	}

	// Return Error WhatsApp Client is not Valid
	return errors.New("WhatsAppLogout WhatsApp Client is not Valid")
}

func WhatsAppIsClientOK(user *WhatsAppTenantUser) error {
	// Make Sure WhatsApp Client is Connected
	if !WhatsAppActiveTenantClient[user.UserToken].Conn.IsConnected() {
		return errors.New("WhatsApp Client is not Connected")
	}

	// Make Sure WhatsApp Client is Logged In
	if !WhatsAppActiveTenantClient[user.UserToken].Conn.IsLoggedIn() {
		return errors.New("WhatsApp Client is not Logged In")
	}

	return nil
}

func WhatsAppGetJID(user *WhatsAppTenantUser, id string) types.JID {
	if WhatsAppActiveTenantClient[user.UserToken] != nil {
		var ids []string

		ids = append(ids, "+"+id)
		infos, err := WhatsAppActiveTenantClient[user.UserToken].Conn.IsOnWhatsApp(ids)
		if err == nil {
			// If WhatsApp ID is Registered Then
			// Return ID Information
			if infos[0].IsIn {
				return infos[0].JID
			}
		}
	}

	// Return Empty ID Information
	return types.EmptyJID
}

func WhatsAppCheckJID(user *WhatsAppTenantUser, id string) (types.JID, error) {
	if WhatsAppActiveTenantClient[user.UserToken] != nil {
		// Compose New Remote JID
		remoteJID := WhatsAppComposeJID(id)
		if remoteJID.Server != types.GroupServer {
			// Validate JID if Remote JID is not Group JID
			if WhatsAppGetJID(user, remoteJID.String()).IsEmpty() {
				return types.EmptyJID, errors.New("WhatsApp Personal ID is Not Registered")
			}
		}

		// Return Remote ID Information
		return remoteJID, nil
	}

	// Return Empty ID Information
	return types.EmptyJID, nil
}

func WhatsAppComposeJID(id string) types.JID {
	// Decompose WhatsApp ID First Before Recomposing
	id = WhatsAppDecomposeJID(id)

	// Check if ID is Group or Not By Detecting '-' for Old Group ID
	// Or By ID Length That Should be 18 Digits or More
	if strings.ContainsRune(id, '-') || len(id) >= 18 {
		// Return New Group User JID
		return types.NewJID(id, types.GroupServer)
	}

	// Return New Standard User JID
	return types.NewJID(id, types.DefaultUserServer)
}

func WhatsAppDecomposeJID(id string) string {
	// Check if WhatsApp ID Contains '@' Symbol
	if strings.ContainsRune(id, '@') {
		// Split WhatsApp ID Based on '@' Symbol
		// and Get Only The First Section Before The Symbol
		buffers := strings.Split(id, "@")
		id = buffers[0]
	}

	// Check if WhatsApp ID First Character is '+' Symbol
	if id[0] == '+' {
		// Remove '+' Symbol from WhatsApp ID
		id = id[1:]
	}

	return id
}

func WhatsAppPresence(user *WhatsAppTenantUser, isAvailable bool) {
	if isAvailable {
		_ = WhatsAppActiveTenantClient[user.UserToken].Conn.SendPresence(types.PresenceAvailable)
	} else {
		_ = WhatsAppActiveTenantClient[user.UserToken].Conn.SendPresence(types.PresenceUnavailable)
	}
}

func WhatsAppComposeStatus(user *WhatsAppTenantUser, rjid types.JID, isComposing bool, isAudio bool) {
	// Set Compose Status
	var typeCompose types.ChatPresence
	if isComposing {
		typeCompose = types.ChatPresenceComposing
	} else {
		typeCompose = types.ChatPresencePaused
	}

	// Set Compose Media Audio (Recording) or Text (Typing)
	var typeComposeMedia types.ChatPresenceMedia
	if isAudio {
		typeComposeMedia = types.ChatPresenceMediaAudio
	} else {
		typeComposeMedia = types.ChatPresenceMediaText
	}

	// Send Chat Compose Status
	_ = WhatsAppActiveTenantClient[user.UserToken].Conn.SendChatPresence(rjid, typeCompose, typeComposeMedia)
}

func WhatsAppCheckRegistered(user *WhatsAppTenantUser, id string) error {
	if WhatsAppActiveTenantClient[user.UserToken] != nil {
		var err error

		// Make Sure WhatsApp Client is OK
		err = WhatsAppIsClientOK(user)
		if err != nil {
			return err
		}

		// Make Sure WhatsApp ID is Registered
		remoteJID, err := WhatsAppCheckJID(user, id)
		if err != nil {
			return err
		}

		// Make Sure WhatsApp ID is Not Empty or It is Not Group ID
		if remoteJID.IsEmpty() || remoteJID.Server == types.GroupServer {
			return errors.New("WhatsApp Personal ID is Not Registered")
		}

		return nil
	}

	// Return Error WhatsApp Client is not Valid
	return errors.New("WhatsAppCheckRegistered WhatsApp Client is not Valid")
}

func WhatsAppSendText(ctx context.Context, user *WhatsAppTenantUser, rjid string, message string) (string, error) {
	if WhatsAppActiveTenantClient[user.UserToken] != nil {
		var err error

		// Make Sure WhatsApp Client is OK
		err = WhatsAppIsClientOK(user)
		if err != nil {
			return "", err
		}

		// Make Sure WhatsApp ID is Registered
		remoteJID, err := WhatsAppCheckJID(user, rjid)
		if err != nil {
			return "", err
		}

		// Set Chat Presence
		WhatsAppPresence(user, true)
		WhatsAppComposeStatus(user, remoteJID, true, false)
		defer func() {
			WhatsAppComposeStatus(user, remoteJID, false, false)
			WhatsAppPresence(user, false)
		}()

		// Compose WhatsApp Proto
		msgExtra := whatsmeow.SendRequestExtra{
			ID: WhatsAppActiveTenantClient[user.UserToken].Conn.GenerateMessageID(),
		}
		msgContent := &waE2E.Message{
			Conversation: proto.String(message),
		}

		// Send WhatsApp Message Proto
		_, err = WhatsAppActiveTenantClient[user.UserToken].Conn.SendMessage(ctx, remoteJID, msgContent, msgExtra)
		if err != nil {
			return "", err
		}

		return msgExtra.ID, nil
	}

	// Return Error WhatsApp Client is not Valid
	return "", errors.New("WhatsAppSendText WhatsApp Client is not Valid")
}

func WhatsAppSendLocation(ctx context.Context, user *WhatsAppTenantUser, rjid string, latitude float64, longitude float64) (string, error) {
	if WhatsAppActiveTenantClient[user.UserToken] != nil {
		var err error

		// Make Sure WhatsApp Client is OK
		err = WhatsAppIsClientOK(user)
		if err != nil {
			return "", err
		}

		// Make Sure WhatsApp ID is Registered
		remoteJID, err := WhatsAppCheckJID(user, rjid)
		if err != nil {
			return "", err
		}

		// Set Chat Presence
		WhatsAppPresence(user, true)
		WhatsAppComposeStatus(user, remoteJID, true, false)
		defer func() {
			WhatsAppComposeStatus(user, remoteJID, false, false)
			WhatsAppPresence(user, false)
		}()

		// Compose WhatsApp Proto
		msgExtra := whatsmeow.SendRequestExtra{
			ID: WhatsAppActiveTenantClient[user.UserToken].Conn.GenerateMessageID(),
		}
		msgContent := &waE2E.Message{
			LocationMessage: &waE2E.LocationMessage{
				DegreesLatitude:  proto.Float64(latitude),
				DegreesLongitude: proto.Float64(longitude),
			},
		}

		// Send WhatsApp Message Proto
		_, err = WhatsAppActiveTenantClient[user.UserToken].Conn.SendMessage(ctx, remoteJID, msgContent, msgExtra)
		if err != nil {
			return "", err
		}

		return msgExtra.ID, nil
	}

	// Return Error WhatsApp Client is not Valid
	return "", errors.New("WhatsApp Client is not Valid")
}

func WhatsAppSendDocument(ctx context.Context, user *WhatsAppTenantUser, rjid string, fileBytes []byte, fileType string, fileName string) (string, error) {
	if WhatsAppActiveTenantClient[user.UserToken] != nil {
		var err error

		// Make Sure WhatsApp Client is OK
		err = WhatsAppIsClientOK(user)
		if err != nil {
			return "", err
		}

		// Make Sure WhatsApp ID is Registered
		remoteJID, err := WhatsAppCheckJID(user, rjid)
		if err != nil {
			return "", err
		}

		// Set Chat Presence
		WhatsAppPresence(user, true)
		WhatsAppComposeStatus(user, remoteJID, true, false)
		defer func() {
			WhatsAppComposeStatus(user, remoteJID, false, false)
			WhatsAppPresence(user, false)
		}()

		// Upload File to WhatsApp Storage Server
		fileUploaded, err := WhatsAppActiveTenantClient[user.UserToken].Conn.Upload(ctx, fileBytes, whatsmeow.MediaDocument)
		if err != nil {
			return "", errors.New("Error While Uploading Media to WhatsApp Server")
		}

		// Compose WhatsApp Proto
		msgExtra := whatsmeow.SendRequestExtra{
			ID: WhatsAppActiveTenantClient[user.UserToken].Conn.GenerateMessageID(),
		}
		msgContent := &waE2E.Message{
			DocumentMessage: &waE2E.DocumentMessage{
				URL:           proto.String(fileUploaded.URL),
				DirectPath:    proto.String(fileUploaded.DirectPath),
				Mimetype:      proto.String(fileType),
				Title:         proto.String(fileName),
				FileName:      proto.String(fileName),
				FileLength:    proto.Uint64(fileUploaded.FileLength),
				FileSHA256:    fileUploaded.FileSHA256,
				FileEncSHA256: fileUploaded.FileEncSHA256,
				MediaKey:      fileUploaded.MediaKey,
			},
		}

		// Send WhatsApp Message Proto
		_, err = WhatsAppActiveTenantClient[user.UserToken].Conn.SendMessage(ctx, remoteJID, msgContent, msgExtra)
		if err != nil {
			return "", err
		}

		return msgExtra.ID, nil
	}

	// Return Error WhatsApp Client is not Valid
	return "", errors.New("WhatsApp Client is not Valid")
}

func WhatsAppSendImage(ctx context.Context, user *WhatsAppTenantUser, rjid string, imageBytes []byte, imageType string, imageCaption string, isViewOnce bool) (string, error) {
	if WhatsAppActiveTenantClient[user.UserToken] != nil {
		var err error

		// Make Sure WhatsApp Client is OK
		err = WhatsAppIsClientOK(user)
		if err != nil {
			return "", err
		}

		// Make Sure WhatsApp ID is Registered
		remoteJID, err := WhatsAppCheckJID(user, rjid)
		if err != nil {
			return "", err
		}

		// Set Chat Presence
		WhatsAppPresence(user, true)
		WhatsAppComposeStatus(user, remoteJID, true, false)
		defer func() {
			WhatsAppComposeStatus(user, remoteJID, false, false)
			WhatsAppPresence(user, false)
		}()

		// Issue #7 Old Version Client Cannot Render WebP Format
		// If MIME Type is "image/webp" Then Convert it as PNG
		isWhatsAppImageConvertWebP, err := env.GetEnvBool("WHATSAPP_MEDIA_IMAGE_CONVERT_WEBP")
		if err != nil {
			isWhatsAppImageConvertWebP = false
		}

		if imageType == "image/webp" && isWhatsAppImageConvertWebP {
			imgConvDecode, err := imgconv.Decode(bytes.NewReader(imageBytes))
			if err != nil {
				return "", errors.New("Error While Decoding Convert Image Stream")
			}

			imgConvEncode := new(bytes.Buffer)

			err = imgconv.Write(imgConvEncode, imgConvDecode, &imgconv.FormatOption{Format: imgconv.PNG})
			if err != nil {
				return "", errors.New("Error While Encoding Convert Image Stream")
			}

			imageBytes = imgConvEncode.Bytes()
			imageType = "image/png"
		}

		// If WhatsApp Media Compression Enabled
		// Then Resize The Image to Width 1024px and Preserve Aspect Ratio
		isWhatsAppImageCompression, err := env.GetEnvBool("WHATSAPP_MEDIA_IMAGE_COMPRESSION")
		if err != nil {
			isWhatsAppImageCompression = false
		}

		if isWhatsAppImageCompression {
			imgResizeDecode, err := imgconv.Decode(bytes.NewReader(imageBytes))
			if err != nil {
				return "", errors.New("Error While Decoding Resize Image Stream")
			}

			imgResizeEncode := new(bytes.Buffer)

			err = imgconv.Write(imgResizeEncode,
				imgconv.Resize(imgResizeDecode, &imgconv.ResizeOption{Width: 1024}),
				&imgconv.FormatOption{})

			if err != nil {
				return "", errors.New("Error While Encoding Resize Image Stream")
			}

			imageBytes = imgResizeEncode.Bytes()
		}

		// Creating Image JPEG Thumbnail
		// With Permanent Width 640px and Preserve Aspect Ratio
		imgThumbDecode, err := imgconv.Decode(bytes.NewReader(imageBytes))
		if err != nil {
			return "", errors.New("Error While Decoding Thumbnail Image Stream")
		}

		imgThumbEncode := new(bytes.Buffer)

		err = imgconv.Write(imgThumbEncode,
			imgconv.Resize(imgThumbDecode, &imgconv.ResizeOption{Width: 72}),
			&imgconv.FormatOption{Format: imgconv.JPEG})

		if err != nil {
			return "", errors.New("Error While Encoding Thumbnail Image Stream")
		}

		// Upload Image to WhatsApp Storage Server
		imageUploaded, err := WhatsAppActiveTenantClient[user.UserToken].Conn.Upload(ctx, imageBytes, whatsmeow.MediaImage)
		if err != nil {
			return "", errors.New("Error While Uploading Media to WhatsApp Server")
		}

		// Upload Image Thumbnail to WhatsApp Storage Server
		imageThumbUploaded, err := WhatsAppActiveTenantClient[user.UserToken].Conn.Upload(ctx, imgThumbEncode.Bytes(), whatsmeow.MediaLinkThumbnail)
		if err != nil {
			return "", errors.New("Error while Uploading Image Thumbnail to WhatsApp Server")
		}

		// Compose WhatsApp Proto
		msgExtra := whatsmeow.SendRequestExtra{
			ID: WhatsAppActiveTenantClient[user.UserToken].Conn.GenerateMessageID(),
		}
		msgContent := &waE2E.Message{
			ImageMessage: &waE2E.ImageMessage{
				URL:                 proto.String(imageUploaded.URL),
				DirectPath:          proto.String(imageUploaded.DirectPath),
				Mimetype:            proto.String(imageType),
				Caption:             proto.String(imageCaption),
				FileLength:          proto.Uint64(imageUploaded.FileLength),
				FileSHA256:          imageUploaded.FileSHA256,
				FileEncSHA256:       imageUploaded.FileEncSHA256,
				MediaKey:            imageUploaded.MediaKey,
				JPEGThumbnail:       imgThumbEncode.Bytes(),
				ThumbnailDirectPath: &imageThumbUploaded.DirectPath,
				ThumbnailSHA256:     imageThumbUploaded.FileSHA256,
				ThumbnailEncSHA256:  imageThumbUploaded.FileEncSHA256,
				ViewOnce:            proto.Bool(isViewOnce),
			},
		}

		// Send WhatsApp Message Proto
		_, err = WhatsAppActiveTenantClient[user.UserToken].Conn.SendMessage(ctx, remoteJID, msgContent, msgExtra)
		if err != nil {
			return "", err
		}

		return msgExtra.ID, nil
	}

	// Return Error WhatsApp Client is not Valid
	return "", errors.New("WhatsApp Client is not Valid")
}

func WhatsAppSendAudio(ctx context.Context, user *WhatsAppTenantUser, rjid string, audioBytes []byte, audioType string) (string, error) {
	if WhatsAppActiveTenantClient[user.UserToken] != nil {
		var err error

		// Make Sure WhatsApp Client is OK
		err = WhatsAppIsClientOK(user)
		if err != nil {
			return "", err
		}

		// Make Sure WhatsApp ID is Registered
		remoteJID, err := WhatsAppCheckJID(user, rjid)
		if err != nil {
			return "", err
		}

		// Set Chat Presence
		WhatsAppComposeStatus(user, remoteJID, true, true)
		defer WhatsAppComposeStatus(user, remoteJID, false, true)

		// Upload Audio to WhatsApp Storage Server
		audioUploaded, err := WhatsAppActiveTenantClient[user.UserToken].Conn.Upload(ctx, audioBytes, whatsmeow.MediaAudio)
		if err != nil {
			return "", errors.New("Error While Uploading Media to WhatsApp Server")
		}

		// Compose WhatsApp Proto
		msgExtra := whatsmeow.SendRequestExtra{
			ID: WhatsAppActiveTenantClient[user.UserToken].Conn.GenerateMessageID(),
		}
		msgContent := &waE2E.Message{
			AudioMessage: &waE2E.AudioMessage{
				URL:           proto.String(audioUploaded.URL),
				DirectPath:    proto.String(audioUploaded.DirectPath),
				Mimetype:      proto.String(audioType),
				FileLength:    proto.Uint64(audioUploaded.FileLength),
				FileSHA256:    audioUploaded.FileSHA256,
				FileEncSHA256: audioUploaded.FileEncSHA256,
				MediaKey:      audioUploaded.MediaKey,
			},
		}

		// Send WhatsApp Message Proto
		_, err = WhatsAppActiveTenantClient[user.UserToken].Conn.SendMessage(ctx, remoteJID, msgContent, msgExtra)
		if err != nil {
			return "", err
		}

		return msgExtra.ID, nil
	}

	// Return Error WhatsApp Client is not Valid
	return "", errors.New("WhatsApp Client is not Valid")
}

func WhatsAppSendVideo(ctx context.Context, user *WhatsAppTenantUser, rjid string, videoBytes []byte, videoType string, videoCaption string, isViewOnce bool) (string, error) {
	if WhatsAppActiveTenantClient[user.UserToken] != nil {
		var err error

		// Make Sure WhatsApp Client is OK
		err = WhatsAppIsClientOK(user)
		if err != nil {
			return "", err
		}

		// Make Sure WhatsApp ID is Registered
		remoteJID, err := WhatsAppCheckJID(user, rjid)
		if err != nil {
			return "", err
		}

		// Set Chat Presence
		WhatsAppPresence(user, true)
		WhatsAppComposeStatus(user, remoteJID, true, false)
		defer func() {
			WhatsAppComposeStatus(user, remoteJID, false, false)
			WhatsAppPresence(user, false)
		}()

		// Upload Video to WhatsApp Storage Server
		videoUploaded, err := WhatsAppActiveTenantClient[user.UserToken].Conn.Upload(ctx, videoBytes, whatsmeow.MediaVideo)
		if err != nil {
			return "", errors.New("Error While Uploading Media to WhatsApp Server")
		}

		// Compose WhatsApp Proto
		msgExtra := whatsmeow.SendRequestExtra{
			ID: WhatsAppActiveTenantClient[user.UserToken].Conn.GenerateMessageID(),
		}
		msgContent := &waE2E.Message{
			VideoMessage: &waE2E.VideoMessage{
				URL:           proto.String(videoUploaded.URL),
				DirectPath:    proto.String(videoUploaded.DirectPath),
				Mimetype:      proto.String(videoType),
				Caption:       proto.String(videoCaption),
				FileLength:    proto.Uint64(videoUploaded.FileLength),
				FileSHA256:    videoUploaded.FileSHA256,
				FileEncSHA256: videoUploaded.FileEncSHA256,
				MediaKey:      videoUploaded.MediaKey,
				ViewOnce:      proto.Bool(isViewOnce),
			},
		}

		// Send WhatsApp Message Proto
		_, err = WhatsAppActiveTenantClient[user.UserToken].Conn.SendMessage(ctx, remoteJID, msgContent, msgExtra)
		if err != nil {
			return "", err
		}

		return msgExtra.ID, nil
	}

	// Return Error WhatsApp Client is not Valid
	return "", errors.New("WhatsApp Client is not Valid")
}

func WhatsAppSendContact(ctx context.Context, user *WhatsAppTenantUser, rjid string, contactName string, contactNumber string) (string, error) {
	if WhatsAppActiveTenantClient[user.UserToken] != nil {
		var err error

		// Make Sure WhatsApp Client is OK
		err = WhatsAppIsClientOK(user)
		if err != nil {
			return "", err
		}

		// Make Sure WhatsApp ID is Registered
		remoteJID, err := WhatsAppCheckJID(user, rjid)
		if err != nil {
			return "", err
		}

		// Set Chat Presence
		WhatsAppPresence(user, true)
		WhatsAppComposeStatus(user, remoteJID, true, false)
		defer func() {
			WhatsAppComposeStatus(user, remoteJID, false, false)
			WhatsAppPresence(user, false)
		}()

		// Compose WhatsApp Proto
		msgExtra := whatsmeow.SendRequestExtra{
			ID: WhatsAppActiveTenantClient[user.UserToken].Conn.GenerateMessageID(),
		}
		msgVCard := fmt.Sprintf("BEGIN:VCARD\nVERSION:3.0\nN:;%v;;;\nFN:%v\nTEL;type=CELL;waid=%v:+%v\nEND:VCARD",
			contactName, contactName, contactNumber, contactNumber)
		msgContent := &waE2E.Message{
			ContactMessage: &waE2E.ContactMessage{
				DisplayName: proto.String(contactName),
				Vcard:       proto.String(msgVCard),
			},
		}

		// Send WhatsApp Message Proto
		_, err = WhatsAppActiveTenantClient[user.UserToken].Conn.SendMessage(ctx, remoteJID, msgContent, msgExtra)
		if err != nil {
			return "", err
		}

		return msgExtra.ID, nil
	}

	// Return Error WhatsApp Client is not Valid
	return "", errors.New("WhatsApp Client is not Valid")
}

func WhatsAppSendLink(ctx context.Context, user *WhatsAppTenantUser, rjid string, linkCaption string, linkURL string) (string, error) {
	if WhatsAppActiveTenantClient[user.UserToken] != nil {
		var err error
		var urlTitle, urlDescription string

		// Make Sure WhatsApp Client is OK
		err = WhatsAppIsClientOK(user)
		if err != nil {
			return "", err
		}

		// Make Sure WhatsApp ID is Registered
		remoteJID, err := WhatsAppCheckJID(user, rjid)
		if err != nil {
			return "", err
		}

		// Set Chat Presence
		WhatsAppPresence(user, true)
		WhatsAppComposeStatus(user, remoteJID, true, false)
		defer func() {
			WhatsAppComposeStatus(user, remoteJID, false, false)
			WhatsAppPresence(user, false)
		}()

		// Get URL Metadata
		urlResponse, err := http.Get(linkURL)
		if err != nil {
			return "", err
		}
		defer urlResponse.Body.Close()

		if urlResponse.StatusCode != 200 {
			return "", errors.New("Error While Fetching URL Metadata!")
		}

		// Query URL Metadata
		docData, err := goquery.NewDocumentFromReader(urlResponse.Body)
		if err != nil {
			return "", err
		}

		docData.Find("title").Each(func(index int, element *goquery.Selection) {
			urlTitle = element.Text()
		})

		docData.Find("meta[name='description']").Each(func(index int, element *goquery.Selection) {
			urlDescription, _ = element.Attr("content")
		})

		// Compose WhatsApp Proto
		msgExtra := whatsmeow.SendRequestExtra{
			ID: WhatsAppActiveTenantClient[user.UserToken].Conn.GenerateMessageID(),
		}
		msgText := linkURL

		if len(strings.TrimSpace(linkCaption)) > 0 {
			msgText = fmt.Sprintf("%s\n%s", linkCaption, linkURL)
		}

		msgContent := &waE2E.Message{
			ExtendedTextMessage: &waE2E.ExtendedTextMessage{
				Text:         proto.String(msgText),
				Title:        proto.String(urlTitle),
				MatchedText:  proto.String(linkURL),
				CanonicalURL: proto.String(linkURL),
				Description:  proto.String(urlDescription),
			},
		}

		// Send WhatsApp Message Proto
		_, err = WhatsAppActiveTenantClient[user.UserToken].Conn.SendMessage(ctx, remoteJID, msgContent, msgExtra)
		if err != nil {
			return "", err
		}

		return msgExtra.ID, nil
	}

	// Return Error WhatsApp Client is not Valid
	return "", errors.New("WhatsApp Client is not Valid")
}

func WhatsAppSendSticker(ctx context.Context, user *WhatsAppTenantUser, rjid string, stickerBytes []byte) (string, error) {
	if WhatsAppActiveTenantClient[user.UserToken] != nil {
		var err error

		// Make Sure WhatsApp Client is OK
		err = WhatsAppIsClientOK(user)
		if err != nil {
			return "", err
		}

		// Make Sure WhatsApp ID is Registered
		remoteJID, err := WhatsAppCheckJID(user, rjid)
		if err != nil {
			return "", err
		}

		// Set Chat Presence
		WhatsAppPresence(user, true)
		WhatsAppComposeStatus(user, remoteJID, true, false)
		defer func() {
			WhatsAppComposeStatus(user, remoteJID, false, false)
			WhatsAppPresence(user, false)
		}()

		stickerConvDecode, err := imgconv.Decode(bytes.NewReader(stickerBytes))
		if err != nil {
			return "", errors.New("Error While Decoding Convert Sticker Stream")
		}

		stickerConvResize := imgconv.Resize(stickerConvDecode, &imgconv.ResizeOption{Width: 512, Height: 512})
		stickerConvEncode := new(bytes.Buffer)

		err = webp.Encode(stickerConvEncode, stickerConvResize)
		if err != nil {
			return "", errors.New("Error While Encoding Convert Sticker Stream")
		}

		stickerBytes = stickerConvEncode.Bytes()

		// Upload Image to WhatsApp Storage Server
		stickerUploaded, err := WhatsAppActiveTenantClient[user.UserToken].Conn.Upload(ctx, stickerBytes, whatsmeow.MediaImage)
		if err != nil {
			return "", errors.New("Error While Uploading Media to WhatsApp Server")
		}

		// Compose WhatsApp Proto
		msgExtra := whatsmeow.SendRequestExtra{
			ID: WhatsAppActiveTenantClient[user.UserToken].Conn.GenerateMessageID(),
		}
		msgContent := &waE2E.Message{
			StickerMessage: &waE2E.StickerMessage{
				URL:           proto.String(stickerUploaded.URL),
				DirectPath:    proto.String(stickerUploaded.DirectPath),
				Mimetype:      proto.String("image/webp"),
				FileLength:    proto.Uint64(stickerUploaded.FileLength),
				FileSHA256:    stickerUploaded.FileSHA256,
				FileEncSHA256: stickerUploaded.FileEncSHA256,
				MediaKey:      stickerUploaded.MediaKey,
			},
		}

		// Send WhatsApp Message Proto
		_, err = WhatsAppActiveTenantClient[user.UserToken].Conn.SendMessage(ctx, remoteJID, msgContent, msgExtra)
		if err != nil {
			return "", err
		}

		return msgExtra.ID, nil
	}

	// Return Error WhatsApp Client is not Valid
	return "", errors.New("WhatsApp Client is not Valid")
}

func WhatsAppSendPoll(ctx context.Context, user *WhatsAppTenantUser, rjid string, question string, options []string, isMultiAnswer bool) (string, error) {
	if WhatsAppActiveTenantClient[user.UserToken] != nil {
		var err error

		// Make Sure WhatsApp Client is OK
		err = WhatsAppIsClientOK(user)
		if err != nil {
			return "", err
		}

		// Make Sure WhatsApp ID is Registered
		remoteJID, err := WhatsAppCheckJID(user, rjid)
		if err != nil {
			return "", err
		}

		// Set Chat Presence
		WhatsAppPresence(user, true)
		WhatsAppComposeStatus(user, remoteJID, true, false)
		defer func() {
			WhatsAppComposeStatus(user, remoteJID, false, false)
			WhatsAppPresence(user, false)
		}()

		// Check Options Must Be Equal or Greater Than 2
		if len(options) < 2 {
			return "", errors.New("WhatsApp Poll Options / Choices Must Be Equal or Greater Than 2")
		}

		// Check if Poll Allow Multiple Answer
		pollAnswerMax := 1
		if isMultiAnswer {
			pollAnswerMax = len(options)
		}

		// Compose WhatsApp Proto
		msgExtra := whatsmeow.SendRequestExtra{
			ID: WhatsAppActiveTenantClient[user.UserToken].Conn.GenerateMessageID(),
		}

		// Send WhatsApp Message Proto
		_, err = WhatsAppActiveTenantClient[user.UserToken].Conn.SendMessage(ctx, remoteJID, WhatsAppActiveTenantClient[user.UserToken].Conn.BuildPollCreation(question, options, pollAnswerMax), msgExtra)
		if err != nil {
			return "", err
		}

		return msgExtra.ID, nil
	}

	// Return Error WhatsApp Client is not Valid
	return "", errors.New("WhatsApp Client is not Valid")
}

func WhatsAppMessageEdit(ctx context.Context, user *WhatsAppTenantUser, rjid string, msgid string, message string) (string, error) {
	if WhatsAppActiveTenantClient[user.UserToken] != nil {
		var err error

		// Make Sure WhatsApp Client is OK
		err = WhatsAppIsClientOK(user)
		if err != nil {
			return "", err
		}

		// Make Sure WhatsApp ID is Registered
		remoteJID, err := WhatsAppCheckJID(user, rjid)
		if err != nil {
			return "", err
		}

		// Set Chat Presence
		WhatsAppPresence(user, true)
		WhatsAppComposeStatus(user, remoteJID, true, false)
		defer func() {
			WhatsAppComposeStatus(user, remoteJID, false, false)
			WhatsAppPresence(user, false)
		}()

		// Compose WhatsApp Proto
		msgContent := &waE2E.Message{
			Conversation: proto.String(message),
		}

		// Send WhatsApp Message Proto in Edit Mode
		_, err = WhatsAppActiveTenantClient[user.UserToken].Conn.SendMessage(ctx, remoteJID, WhatsAppActiveTenantClient[user.UserToken].Conn.BuildEdit(remoteJID, msgid, msgContent))
		if err != nil {
			return "", err
		}

		return msgid, nil
	}

	// Return Error WhatsApp Client is not Valid
	return "", errors.New("WhatsApp Client is not Valid")
}

func WhatsAppMessageReact(ctx context.Context, user *WhatsAppTenantUser, rjid string, msgid string, emoji string) (string, error) {
	if WhatsAppActiveTenantClient[user.UserToken] != nil {
		var err error

		// Make Sure WhatsApp Client is OK
		err = WhatsAppIsClientOK(user)
		if err != nil {
			return "", err
		}

		// Make Sure WhatsApp ID is Registered
		remoteJID, err := WhatsAppCheckJID(user, rjid)
		if err != nil {
			return "", err
		}

		// Set Chat Presence
		WhatsAppPresence(user, true)
		WhatsAppComposeStatus(user, remoteJID, true, false)
		defer func() {
			WhatsAppComposeStatus(user, remoteJID, false, false)
			WhatsAppPresence(user, false)
		}()

		// Check Emoji Must Be Contain Only 1 Emoji Character
		if !gomoji.ContainsEmoji(emoji) && uniseg.GraphemeClusterCount(emoji) != 1 {
			return "", errors.New("WhatsApp Message React Emoji Must Be Contain Only 1 Emoji Character")
		}

		// Compose WhatsApp Proto
		msgReact := &waE2E.Message{
			ReactionMessage: &waE2E.ReactionMessage{
				Key: &waCommon.MessageKey{
					FromMe:    proto.Bool(true),
					ID:        proto.String(msgid),
					RemoteJID: proto.String(remoteJID.String()),
				},
				Text:              proto.String(emoji),
				SenderTimestampMS: proto.Int64(time.Now().UnixMilli()),
			},
		}

		// Send WhatsApp Message Proto
		_, err = WhatsAppActiveTenantClient[user.UserToken].Conn.SendMessage(ctx, remoteJID, msgReact)
		if err != nil {
			return "", err
		}

		return msgid, nil
	}

	// Return Error WhatsApp Client is not Valid
	return "", errors.New("WhatsApp Client is not Valid")

}

func WhatsAppMessageDelete(ctx context.Context, user *WhatsAppTenantUser, rjid string, msgid string) error {
	if WhatsAppActiveTenantClient[user.UserToken] != nil {
		var err error

		// Make Sure WhatsApp Client is OK
		err = WhatsAppIsClientOK(user)
		if err != nil {
			return err
		}

		// Make Sure WhatsApp ID is Registered
		remoteJID, err := WhatsAppCheckJID(user, rjid)
		if err != nil {
			return err
		}

		// Set Chat Presence
		WhatsAppPresence(user, true)
		WhatsAppComposeStatus(user, remoteJID, true, false)
		defer func() {
			WhatsAppComposeStatus(user, remoteJID, false, false)
			WhatsAppPresence(user, false)
		}()

		// Send WhatsApp Message Proto in Revoke Mode
		_, err = WhatsAppActiveTenantClient[user.UserToken].Conn.SendMessage(ctx, remoteJID, WhatsAppActiveTenantClient[user.UserToken].Conn.BuildRevoke(remoteJID, types.EmptyJID, msgid))
		if err != nil {
			return err
		}

		return nil
	}

	// Return Error WhatsApp Client is not Valid
	return errors.New("WhatsApp Client is not Valid")
}

func WhatsAppGroupGet(user *WhatsAppTenantUser) ([]types.GroupInfo, error) {
	if WhatsAppActiveTenantClient[user.UserToken] != nil {
		var err error

		// Make Sure WhatsApp Client is OK
		err = WhatsAppIsClientOK(user)
		if err != nil {
			return nil, err
		}

		// Get Joined Group List
		groups, err := WhatsAppActiveTenantClient[user.UserToken].Conn.GetJoinedGroups()
		if err != nil {
			return nil, err
		}

		// Put Group Information in List
		var gids []types.GroupInfo
		for _, group := range groups {
			gids = append(gids, *group)
		}

		// Return Group Information List
		return gids, nil
	}

	// Return Error WhatsApp Client is not Valid
	return nil, errors.New("WhatsApp Client is not Valid")
}

func WhatsAppGroupJoin(user *WhatsAppTenantUser, link string) (string, error) {
	if WhatsAppActiveTenantClient[user.UserToken] != nil {
		var err error

		// Make Sure WhatsApp Client is OK
		err = WhatsAppIsClientOK(user)
		if err != nil {
			return "", err
		}

		// Join Group By Invitation Link
		gid, err := WhatsAppActiveTenantClient[user.UserToken].Conn.JoinGroupWithLink(link)
		if err != nil {
			return "", err
		}

		// Return Joined Group ID
		return gid.String(), nil
	}

	// Return Error WhatsApp Client is not Valid
	return "", errors.New("WhatsApp Client is not Valid")
}

func WhatsAppGroupLeave(user *WhatsAppTenantUser, gjid string) error {
	if WhatsAppActiveTenantClient[user.UserToken] != nil {
		var err error

		// Make Sure WhatsApp Client is OK
		err = WhatsAppIsClientOK(user)
		if err != nil {
			return err
		}

		// Make Sure WhatsApp ID is Registered
		groupJID, err := WhatsAppCheckJID(user, gjid)
		if err != nil {
			return err
		}

		// Make Sure WhatsApp ID is Group Server
		if groupJID.Server != types.GroupServer {
			return errors.New("WhatsApp Group ID is Not Group Server")
		}

		// Leave Group By Group ID
		return WhatsAppActiveTenantClient[user.UserToken].Conn.LeaveGroup(groupJID)
	}

	// Return Error WhatsApp Client is not Valid
	return errors.New("WhatsApp Client is not Valid")
}

func initDB() error {
	var err error

	_, err = Db.Exec(`
		CREATE TABLE IF NOT EXISTS whatsmeow_clients (
		  id SERIAL PRIMARY KEY,
		  client_name TEXT NOT NULL,
		  uuid UUID NOT NULL UNIQUE,
		  secret_key TEXT NOT NULL,
		  webhook_url TEXT,
		  status_code INTEGER NOT NULL DEFAULT 1,
		  updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
		  created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
		);
		
		CREATE INDEX IF NOT EXISTS idx_uuid ON whatsmeow_clients (uuid);
		
		CREATE TABLE IF NOT EXISTS whatsmeow_device_client_pivot (
		  id SERIAL PRIMARY KEY,
		  client_id INTEGER NOT NULL,
		  jid TEXT,
		  token TEXT NOT NULL UNIQUE,
		  updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
		  created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
		  CONSTRAINT fk_client FOREIGN KEY (client_id) REFERENCES whatsmeow_clients(id) ON DELETE CASCADE
		);
	`)

	if err != nil {
		return fmt.Errorf("error creating table: %w", err)
	}

	return nil
}

func saveUUID(jid types.JID, user *WhatsAppTenantUser) error {
	_, err := Db.Exec(`
		INSERT INTO whatsmeow_device_client_pivot (jid, token, updated_at, client_id)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT(token) DO UPDATE SET 
			updated_at = EXCLUDED.updated_at;
		`,
		jid, user.UserToken, time.Now(), user.ClientId,
	)
	return err
}

func removeByUUID(user *WhatsAppTenantUser) error {
	_, err := Db.Exec(`
		DELETE FROM whatsmeow_device_client_pivot
		WHERE token = $1
	`, user.UserToken)
	return err
}

type CreateClientRequest struct {
	Name       string `form:"name" validate:"required,min=3,max=50"`
	WebhookURL string `form:"webhook_url" validate:"omitempty,url"`
}

func CreateClient(c echo.Context) error {
	// Parse and validate request
	req := new(CreateClientRequest)
	if err := c.Bind(req); err != nil {
		return router.ResponseBadRequest(c, "Invalid request format")
	}

	if err := c.Validate(req); err != nil {
		return router.ResponseBadRequest(c, err.Error())
	}

	// Generate UUID and secret key
	uuid := generateUUID()
	secretKey := generateSecretKey()

	// Store in database
	var id int64
	err := Db.QueryRow(`
        INSERT INTO whatsmeow_clients 
        (client_name, uuid, secret_key, webhook_url, status_code) 
        VALUES ($1, $2, $3, $4, 1)
        RETURNING id`,
		req.Name, uuid, secretKey, req.WebhookURL,
	).Scan(&id)

	if err != nil {
		fmt.Println(err)
		return router.ResponseInternalError(c, "Failed to create client")
	}

	// Prepare response data
	responseData := map[string]interface{}{
		"id":          id,
		"client_name": req.Name,
		"uuid":        uuid,
		"webhook_url": req.WebhookURL,
		"secretKey":   secretKey,
		"status":      "active",
	}

	return router.ResponseSuccessWithData(c, "Successfully created client", responseData)
}

// Helper functions
func generateUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func generateSecretKey() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	key := base64.URLEncoding.EncodeToString(b)
	keyWithoutUnderscores := strings.ReplaceAll(key, "_", "")
	keyWithoutUnderscores = strings.TrimRight(keyWithoutUnderscores, "=")
	return strings.TrimLeft(keyWithoutUnderscores, "-")
}

// ClientStatusResponse defines the response structure
type ClientStatusResponse struct {
	ID         int64  `json:"id"`
	ClientName string `json:"client_name"`
	UUID       string `json:"uuid"`
	WebhookURL string `json:"webhook_url"`
	SecretKey  string `json:"secret_key"`
	Status     string `json:"status"`
	StatusCode int    `json:"status_code"`
	UserCount  int    `json:"user_count"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

// ClientStatus returns client status with user count
func ClientStatus(c echo.Context) error {
	uuid := c.Param("uuid")

	var response ClientStatusResponse
	err := Db.QueryRow(`
        SELECT 
            c.id, 
            c.client_name, 
            c.uuid, 
            c.webhook_url, 
            c.secret_key,
            c.status_code,
            CASE c.status_code 
                WHEN 1 THEN 'active' 
                WHEN 0 THEN 'inactive' 
                ELSE 'unknown' 
            END as status,
            c.created_at,
            c.updated_at,
            COUNT(p.id) as user_count
        FROM whatsmeow_clients c
        LEFT JOIN whatsmeow_device_client_pivot p ON c.id = p.client_id
        WHERE c.uuid = $1
        GROUP BY c.id`,
		uuid,
	).Scan(
		&response.ID,
		&response.ClientName,
		&response.UUID,
		&response.WebhookURL,
		&response.SecretKey,
		&response.StatusCode,
		&response.Status,
		&response.CreatedAt,
		&response.UpdatedAt,
		&response.UserCount,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return router.ResponseNotFound(c, "Client not found")
		}
		return router.ResponseInternalError(c, "Failed to get client status")
	}

	return router.ResponseSuccessWithData(c, "Client status retrieved", response)
}

// ClientStatusEdit updates client status
type UpdateClientStatusRequest struct {
	StatusCode string `form:"status_code" validate:"required,oneof=0 1"`
}

func ClientStatusEdit(c echo.Context) error {
	uuid := c.Param("uuid")

	// Parse form data
	req := new(UpdateClientStatusRequest)
	if err := c.Bind(req); err != nil {
		return router.ResponseBadRequest(c, "Invalid request format")
	}

	// Convert string form value to int
	statusStr := c.FormValue("status_code")
	statusCode, err := strconv.Atoi(statusStr)
	if err != nil {
		return router.ResponseBadRequest(c, "status_code must be a number (0 or 1)")
	}

	// Validate
	if err := c.Validate(req); err != nil {
		return router.ResponseBadRequest(c, err.Error())
	}

	// Update status in database
	result, err := Db.Exec(`
        UPDATE whatsmeow_clients 
        SET status_code = $1, 
            updated_at = CURRENT_TIMESTAMP
        WHERE uuid = $2`,
		statusCode,
		uuid,
	)
	if err != nil {
		return router.ResponseInternalError(c, "Failed to update client status")
	}

	// Check if client was found
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return router.ResponseInternalError(c, "Failed to verify update")
	}

	if rowsAffected == 0 {
		return router.ResponseNotFound(c, "Client not found")
	}

	// Return updated client status
	return ClientStatus(c) // Reuse the GET endpoint to return updated status
}

// ClientDelete handles client deletion
func ClientDelete(c echo.Context) error {
	uuid := c.Param("uuid")

	tx, err := Db.Begin()
	if err != nil {
		return router.ResponseInternalError(c, "Failed to start transaction")
	}

	// Delete client (ON DELETE CASCADE will handle related pivot rows)
	var deletedID int64
	err = tx.QueryRow(`
		DELETE FROM whatsmeow_clients 
		WHERE uuid = $1 
		RETURNING id
	`, uuid).Scan(&deletedID)

	if err == sql.ErrNoRows {
		tx.Rollback()
		return router.ResponseNotFound(c, "Client not found")
	} else if err != nil {
		tx.Rollback()
		return router.ResponseInternalError(c, "Failed to delete client")
	}

	if err := tx.Commit(); err != nil {
		return router.ResponseInternalError(c, "Failed to complete deletion")
	}

	return router.ResponseSuccess(c, "Client deleted successfully")
}

func GetWhatsAppUserWithToken(uuid string, clientName string, clientPassword string) (*WhatsAppTenantUser, error) {
	var (
		user       WhatsAppTenantUser
		jid        sql.NullString
		webhookURL sql.NullString
	)

	query1 := `
		SELECT 
			p.jid, 
			c.webhook_url, 
			c.status_code, 
			c.id AS client_id
		FROM whatsmeow_device_client_pivot p
		JOIN whatsmeow_clients c ON p.client_id = c.id
		WHERE p.token = $1
			AND c.uuid = $2
			AND c.secret_key = $3
			AND c.status_code = 1
		LIMIT 1
	`

	query2 := `
		SELECT 
			c.webhook_url, 
			c.status_code, 
			c.id AS client_id
		FROM whatsmeow_clients c
		WHERE c.uuid = $1
			AND c.secret_key = $2
			AND c.status_code = 1
		LIMIT 1
	`

	err := Db.QueryRow(query1, uuid, clientName, clientPassword).Scan(
		&jid,
		&webhookURL,
		&user.StatusCode,
		&user.ClientId,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			// Try fallback query if no device-user match found
			err = Db.QueryRow(query2, clientName, clientPassword).Scan(
				&webhookURL,
				&user.StatusCode,
				&user.ClientId,
			)
			if err != nil {
				if err == sql.ErrNoRows {
					return nil, fmt.Errorf("user not found")
				}
				return nil, fmt.Errorf("database error: %w", err)
			}
			jid = sql.NullString{} // No device row means no JID
		} else {
			return nil, fmt.Errorf("database error: %w", err)
		}
	}

	// Convert nullable fields to Go strings
	if jid.Valid {
		user.JID = jid.String
	}
	if webhookURL.Valid {
		user.WebhookURL = webhookURL.String
	}

	user.UserToken = uuid

	return &user, nil
}

// IsValidHTTPSURL checks if a given string is a valid HTTPS URL.
func IsValidHTTPSURL(urlString string) bool {
	// Parse the URL
	parsedURL, err := url.ParseRequestURI(urlString)
	if err != nil {
		return false // Invalid URL format
	}

	// Check if the scheme is "https" (case-insensitive)
	if strings.ToLower(parsedURL.Scheme) != "https" {
		return false // Not an HTTPS URL
	}

	// Check if the host is present
	if parsedURL.Host == "" {
		return false // Missing host
	}

	return true // It's a valid HTTPS URL
}

// Webhook sender function
func sendWebhookEvent(eventType string, user WhatsAppTenantUser) {
	if user.WebhookURL != "" && IsValidHTTPSURL(user.WebhookURL) {

		payload := map[string]interface{}{
			"event":     eventType,
			"apiKey":    user.UserToken,
			"JID":       user.JID,
			"client_id": user.ClientId,
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		}

		jsonData, err := json.Marshal(payload)
		if err != nil {
			log.Print(nil).Info(fmt.Sprintf("error marshaling webhook payload: %w", err))
		}

		resp, err := http.Post(user.WebhookURL, "application/json", bytes.NewBuffer(jsonData))
		if err != nil {
			log.Print(nil).Info(fmt.Sprintf("error sending webhook: %w", err))
		}

		defer func(Body io.ReadCloser) {
			err := Body.Close()
			if err != nil {

			}
		}(resp.Body)

		if resp.StatusCode >= 400 {
			log.Print(nil).Info(fmt.Sprintf("webhook returned status %d", resp.StatusCode))
		}

	}
}
