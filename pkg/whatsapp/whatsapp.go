package whatsapp

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/labstack/echo/v4"
	"github.com/rakibhoossain/go-whatsapp-multidevice-rest/pkg/router"
	"go.mau.fi/whatsmeow/types/events"
	"net/http"
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

var WhatsAppDatastore *sqlstore.Container
var WhatsAppClient = make(map[string]*whatsmeow.Client)
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
		log.Print(nil).Fatal("Error Connect WhatsApp Client Datastore")
	}

	Db, err = sql.Open(dbType, dbURI)
	if err != nil {
		log.Print(nil).Fatal("Error Connect WhatsApp Client Datastore")
	}

	err = initDB()
	if err != nil {
		return
	}

	WhatsAppClientProxyURL, _ = env.GetEnvString("WHATSAPP_CLIENT_PROXY_URL")

	WhatsAppDatastore = datastore
}

func WhatsAppInitClient(device *store.Device, jid string) {
	var err error
	wabin.IndentXML = true

	if WhatsAppClient[jid] == nil {
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
		WhatsAppClient[jid] = whatsmeow.NewClient(device, nil)
		WhatsAppClient[jid].AddEventHandler(createEventHandler(jid))

		// Set WhatsApp Client Proxy Address if Proxy URL is Provided
		if len(WhatsAppClientProxyURL) > 0 {
			WhatsAppClient[jid].SetProxyAddress(WhatsAppClientProxyURL)
		}

		// Set WhatsApp Client Auto Reconnect
		WhatsAppClient[jid].EnableAutoReconnect = true

		// Set WhatsApp Client Auto Trust Identity
		WhatsAppClient[jid].AutoTrustIdentity = true
	}
}

func createEventHandler(jid string) func(interface{}) {
	return func(evt interface{}) {
		switch v := evt.(type) {
		case *events.PairSuccess:
			handlePairedEvent(jid, v)
		case *events.LoggedOut:
			handleLoggedOutEvent(jid)
		}
	}
}

func handlePairedEvent(jid string, evt *events.PairSuccess) {
	err := saveUUID(evt.ID, jid)
	if err != nil {
		fmt.Printf("connected: JID store failed JID: %s, UUID: %s", evt.ID, jid, evt.ID.User)
		return
	}
	fmt.Printf("connected: JID: %s, UUID: %s", evt.ID, jid, evt.ID.User)
}

func handleLoggedOutEvent(jid string) {
	err := removeByUUID(jid)
	if err != nil {
		fmt.Printf("logout failed UUID: %s", jid)
		return
	}
	fmt.Printf("logout UUID: %s", jid)
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

func WhatsAppGenerateQR(qrChan <-chan whatsmeow.QRChannelItem, jid string) (string, int) {
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

func WhatsAppLogin(jid string) (string, int, error) {
	if WhatsAppClient[jid] != nil {
		// Make Sure WebSocket Connection is Disconnected
		WhatsAppClient[jid].Disconnect()

		if WhatsAppClient[jid].Store.ID == nil {
			// Device ID is not Exist
			// Generate QR Code
			qrChanGenerate, _ := WhatsAppClient[jid].GetQRChannel(context.Background())

			// Connect WebSocket while Initialize QR Code Data to be Sent
			err := WhatsAppClient[jid].Connect()
			if err != nil {
				return "", 0, err
			}

			// Get Generated QR Code and Timeout Information
			qrImage, qrTimeout := WhatsAppGenerateQR(qrChanGenerate, jid)

			// Return QR Code in Base64 Format and Timeout Information
			return "data:image/png;base64," + qrImage, qrTimeout, nil
		} else {
			// Device ID is Exist
			// Reconnect WebSocket
			err := WhatsAppReconnect(jid)
			if err != nil {
				return "", 0, err
			}

			return "WhatsApp Client is Reconnected", 0, nil
		}
	}

	// Return Error WhatsApp Client is not Valid
	return "", 0, errors.New("WhatsAppLogin WhatsApp Client is not Valid")
}

func WhatsAppLoginPair(jid string) (string, int, error) {
	if WhatsAppClient[jid] != nil {
		// Make Sure WebSocket Connection is Disconnected
		WhatsAppClient[jid].Disconnect()

		if WhatsAppClient[jid].Store.ID == nil {
			// Connect WebSocket while also Requesting Pairing Code
			err := WhatsAppClient[jid].Connect()
			if err != nil {
				return "", 0, err
			}

			// Request Pairing Code
			code, err := WhatsAppClient[jid].PairPhone(jid, true, whatsmeow.PairClientChrome, "Chrome ("+WhatsAppGetUserOS()+")")
			if err != nil {
				return "", 0, err
			}

			return code, 160, nil
		} else {
			// Device ID is Exist
			// Reconnect WebSocket
			err := WhatsAppReconnect(jid)
			if err != nil {
				return "", 0, err
			}

			return "WhatsApp Client is Reconnected", 0, nil
		}
	}

	// Return Error WhatsApp Client is not Valid
	return "", 0, errors.New("WhatsAppLoginPair WhatsApp Client is not Valid")
}

func WhatsAppReconnect(jid string) error {
	if WhatsAppClient[jid] != nil {
		// Make Sure WebSocket Connection is Disconnected
		WhatsAppClient[jid].Disconnect()

		// Make Sure Store ID is not Empty
		// To do Reconnection
		if WhatsAppClient[jid] != nil {
			err := WhatsAppClient[jid].Connect()
			if err != nil {
				return err
			}

			return nil
		}

		return errors.New("WhatsApp Client Store ID is Empty, Please Re-Login and Scan QR Code Again")
	}

	return errors.New("WhatsAppReconnect WhatsApp Client is not Valid")
}

func WhatsAppLogout(jid string) error {
	if WhatsAppClient[jid] != nil {
		// Make Sure Store ID is not Empty
		if WhatsAppClient[jid] != nil {
			var err error

			// Set WhatsApp Client Presence to Unavailable
			WhatsAppPresence(jid, false)

			// Logout WhatsApp Client and Disconnect from WebSocket
			err = WhatsAppClient[jid].Logout()
			if err != nil {
				// Force Disconnect
				WhatsAppClient[jid].Disconnect()

				// Manually Delete Device from Datastore Store
				err = WhatsAppClient[jid].Store.Delete()
				if err != nil {
					return err
				}
			}

			// Free WhatsApp Client Map
			WhatsAppClient[jid] = nil
			delete(WhatsAppClient, jid)

			return nil
		}

		return errors.New("WhatsApp Client Store ID is Empty, Please Re-Login and Scan QR Code Again")
	}

	// Return Error WhatsApp Client is not Valid
	return errors.New("WhatsAppLogout WhatsApp Client is not Valid")
}

func WhatsAppIsClientOK(jid string) error {
	// Make Sure WhatsApp Client is Connected
	if !WhatsAppClient[jid].IsConnected() {
		return errors.New("WhatsApp Client is not Connected")
	}

	// Make Sure WhatsApp Client is Logged In
	if !WhatsAppClient[jid].IsLoggedIn() {
		return errors.New("WhatsApp Client is not Logged In")
	}

	return nil
}

func WhatsAppGetJID(jid string, id string) types.JID {
	if WhatsAppClient[jid] != nil {
		var ids []string

		ids = append(ids, "+"+id)
		infos, err := WhatsAppClient[jid].IsOnWhatsApp(ids)
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

func WhatsAppCheckJID(jid string, id string) (types.JID, error) {
	if WhatsAppClient[jid] != nil {
		// Compose New Remote JID
		remoteJID := WhatsAppComposeJID(id)
		if remoteJID.Server != types.GroupServer {
			// Validate JID if Remote JID is not Group JID
			if WhatsAppGetJID(jid, remoteJID.String()).IsEmpty() {
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

func WhatsAppPresence(jid string, isAvailable bool) {
	if isAvailable {
		_ = WhatsAppClient[jid].SendPresence(types.PresenceAvailable)
	} else {
		_ = WhatsAppClient[jid].SendPresence(types.PresenceUnavailable)
	}
}

func WhatsAppComposeStatus(jid string, rjid types.JID, isComposing bool, isAudio bool) {
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
	_ = WhatsAppClient[jid].SendChatPresence(rjid, typeCompose, typeComposeMedia)
}

func WhatsAppCheckRegistered(jid string, id string) error {
	if WhatsAppClient[jid] != nil {
		var err error

		// Make Sure WhatsApp Client is OK
		err = WhatsAppIsClientOK(jid)
		if err != nil {
			return err
		}

		// Make Sure WhatsApp ID is Registered
		remoteJID, err := WhatsAppCheckJID(jid, id)
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

func WhatsAppSendText(ctx context.Context, jid string, rjid string, message string) (string, error) {
	if WhatsAppClient[jid] != nil {
		var err error

		// Make Sure WhatsApp Client is OK
		err = WhatsAppIsClientOK(jid)
		if err != nil {
			return "", err
		}

		// Make Sure WhatsApp ID is Registered
		remoteJID, err := WhatsAppCheckJID(jid, rjid)
		if err != nil {
			return "", err
		}

		// Set Chat Presence
		WhatsAppPresence(jid, true)
		WhatsAppComposeStatus(jid, remoteJID, true, false)
		defer func() {
			WhatsAppComposeStatus(jid, remoteJID, false, false)
			WhatsAppPresence(jid, false)
		}()

		// Compose WhatsApp Proto
		msgExtra := whatsmeow.SendRequestExtra{
			ID: WhatsAppClient[jid].GenerateMessageID(),
		}
		msgContent := &waE2E.Message{
			Conversation: proto.String(message),
		}

		// Send WhatsApp Message Proto
		_, err = WhatsAppClient[jid].SendMessage(ctx, remoteJID, msgContent, msgExtra)
		if err != nil {
			return "", err
		}

		return msgExtra.ID, nil
	}

	// Return Error WhatsApp Client is not Valid
	return "", errors.New("WhatsAppSendText WhatsApp Client is not Valid")
}

func WhatsAppSendLocation(ctx context.Context, jid string, rjid string, latitude float64, longitude float64) (string, error) {
	if WhatsAppClient[jid] != nil {
		var err error

		// Make Sure WhatsApp Client is OK
		err = WhatsAppIsClientOK(jid)
		if err != nil {
			return "", err
		}

		// Make Sure WhatsApp ID is Registered
		remoteJID, err := WhatsAppCheckJID(jid, rjid)
		if err != nil {
			return "", err
		}

		// Set Chat Presence
		WhatsAppPresence(jid, true)
		WhatsAppComposeStatus(jid, remoteJID, true, false)
		defer func() {
			WhatsAppComposeStatus(jid, remoteJID, false, false)
			WhatsAppPresence(jid, false)
		}()

		// Compose WhatsApp Proto
		msgExtra := whatsmeow.SendRequestExtra{
			ID: WhatsAppClient[jid].GenerateMessageID(),
		}
		msgContent := &waE2E.Message{
			LocationMessage: &waE2E.LocationMessage{
				DegreesLatitude:  proto.Float64(latitude),
				DegreesLongitude: proto.Float64(longitude),
			},
		}

		// Send WhatsApp Message Proto
		_, err = WhatsAppClient[jid].SendMessage(ctx, remoteJID, msgContent, msgExtra)
		if err != nil {
			return "", err
		}

		return msgExtra.ID, nil
	}

	// Return Error WhatsApp Client is not Valid
	return "", errors.New("WhatsApp Client is not Valid")
}

func WhatsAppSendDocument(ctx context.Context, jid string, rjid string, fileBytes []byte, fileType string, fileName string) (string, error) {
	if WhatsAppClient[jid] != nil {
		var err error

		// Make Sure WhatsApp Client is OK
		err = WhatsAppIsClientOK(jid)
		if err != nil {
			return "", err
		}

		// Make Sure WhatsApp ID is Registered
		remoteJID, err := WhatsAppCheckJID(jid, rjid)
		if err != nil {
			return "", err
		}

		// Set Chat Presence
		WhatsAppPresence(jid, true)
		WhatsAppComposeStatus(jid, remoteJID, true, false)
		defer func() {
			WhatsAppComposeStatus(jid, remoteJID, false, false)
			WhatsAppPresence(jid, false)
		}()

		// Upload File to WhatsApp Storage Server
		fileUploaded, err := WhatsAppClient[jid].Upload(ctx, fileBytes, whatsmeow.MediaDocument)
		if err != nil {
			return "", errors.New("Error While Uploading Media to WhatsApp Server")
		}

		// Compose WhatsApp Proto
		msgExtra := whatsmeow.SendRequestExtra{
			ID: WhatsAppClient[jid].GenerateMessageID(),
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
		_, err = WhatsAppClient[jid].SendMessage(ctx, remoteJID, msgContent, msgExtra)
		if err != nil {
			return "", err
		}

		return msgExtra.ID, nil
	}

	// Return Error WhatsApp Client is not Valid
	return "", errors.New("WhatsApp Client is not Valid")
}

func WhatsAppSendImage(ctx context.Context, jid string, rjid string, imageBytes []byte, imageType string, imageCaption string, isViewOnce bool) (string, error) {
	if WhatsAppClient[jid] != nil {
		var err error

		// Make Sure WhatsApp Client is OK
		err = WhatsAppIsClientOK(jid)
		if err != nil {
			return "", err
		}

		// Make Sure WhatsApp ID is Registered
		remoteJID, err := WhatsAppCheckJID(jid, rjid)
		if err != nil {
			return "", err
		}

		// Set Chat Presence
		WhatsAppPresence(jid, true)
		WhatsAppComposeStatus(jid, remoteJID, true, false)
		defer func() {
			WhatsAppComposeStatus(jid, remoteJID, false, false)
			WhatsAppPresence(jid, false)
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
		imageUploaded, err := WhatsAppClient[jid].Upload(ctx, imageBytes, whatsmeow.MediaImage)
		if err != nil {
			return "", errors.New("Error While Uploading Media to WhatsApp Server")
		}

		// Upload Image Thumbnail to WhatsApp Storage Server
		imageThumbUploaded, err := WhatsAppClient[jid].Upload(ctx, imgThumbEncode.Bytes(), whatsmeow.MediaLinkThumbnail)
		if err != nil {
			return "", errors.New("Error while Uploading Image Thumbnail to WhatsApp Server")
		}

		// Compose WhatsApp Proto
		msgExtra := whatsmeow.SendRequestExtra{
			ID: WhatsAppClient[jid].GenerateMessageID(),
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
		_, err = WhatsAppClient[jid].SendMessage(ctx, remoteJID, msgContent, msgExtra)
		if err != nil {
			return "", err
		}

		return msgExtra.ID, nil
	}

	// Return Error WhatsApp Client is not Valid
	return "", errors.New("WhatsApp Client is not Valid")
}

func WhatsAppSendAudio(ctx context.Context, jid string, rjid string, audioBytes []byte, audioType string) (string, error) {
	if WhatsAppClient[jid] != nil {
		var err error

		// Make Sure WhatsApp Client is OK
		err = WhatsAppIsClientOK(jid)
		if err != nil {
			return "", err
		}

		// Make Sure WhatsApp ID is Registered
		remoteJID, err := WhatsAppCheckJID(jid, rjid)
		if err != nil {
			return "", err
		}

		// Set Chat Presence
		WhatsAppComposeStatus(jid, remoteJID, true, true)
		defer WhatsAppComposeStatus(jid, remoteJID, false, true)

		// Upload Audio to WhatsApp Storage Server
		audioUploaded, err := WhatsAppClient[jid].Upload(ctx, audioBytes, whatsmeow.MediaAudio)
		if err != nil {
			return "", errors.New("Error While Uploading Media to WhatsApp Server")
		}

		// Compose WhatsApp Proto
		msgExtra := whatsmeow.SendRequestExtra{
			ID: WhatsAppClient[jid].GenerateMessageID(),
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
		_, err = WhatsAppClient[jid].SendMessage(ctx, remoteJID, msgContent, msgExtra)
		if err != nil {
			return "", err
		}

		return msgExtra.ID, nil
	}

	// Return Error WhatsApp Client is not Valid
	return "", errors.New("WhatsApp Client is not Valid")
}

func WhatsAppSendVideo(ctx context.Context, jid string, rjid string, videoBytes []byte, videoType string, videoCaption string, isViewOnce bool) (string, error) {
	if WhatsAppClient[jid] != nil {
		var err error

		// Make Sure WhatsApp Client is OK
		err = WhatsAppIsClientOK(jid)
		if err != nil {
			return "", err
		}

		// Make Sure WhatsApp ID is Registered
		remoteJID, err := WhatsAppCheckJID(jid, rjid)
		if err != nil {
			return "", err
		}

		// Set Chat Presence
		WhatsAppPresence(jid, true)
		WhatsAppComposeStatus(jid, remoteJID, true, false)
		defer func() {
			WhatsAppComposeStatus(jid, remoteJID, false, false)
			WhatsAppPresence(jid, false)
		}()

		// Upload Video to WhatsApp Storage Server
		videoUploaded, err := WhatsAppClient[jid].Upload(ctx, videoBytes, whatsmeow.MediaVideo)
		if err != nil {
			return "", errors.New("Error While Uploading Media to WhatsApp Server")
		}

		// Compose WhatsApp Proto
		msgExtra := whatsmeow.SendRequestExtra{
			ID: WhatsAppClient[jid].GenerateMessageID(),
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
		_, err = WhatsAppClient[jid].SendMessage(ctx, remoteJID, msgContent, msgExtra)
		if err != nil {
			return "", err
		}

		return msgExtra.ID, nil
	}

	// Return Error WhatsApp Client is not Valid
	return "", errors.New("WhatsApp Client is not Valid")
}

func WhatsAppSendContact(ctx context.Context, jid string, rjid string, contactName string, contactNumber string) (string, error) {
	if WhatsAppClient[jid] != nil {
		var err error

		// Make Sure WhatsApp Client is OK
		err = WhatsAppIsClientOK(jid)
		if err != nil {
			return "", err
		}

		// Make Sure WhatsApp ID is Registered
		remoteJID, err := WhatsAppCheckJID(jid, rjid)
		if err != nil {
			return "", err
		}

		// Set Chat Presence
		WhatsAppPresence(jid, true)
		WhatsAppComposeStatus(jid, remoteJID, true, false)
		defer func() {
			WhatsAppComposeStatus(jid, remoteJID, false, false)
			WhatsAppPresence(jid, false)
		}()

		// Compose WhatsApp Proto
		msgExtra := whatsmeow.SendRequestExtra{
			ID: WhatsAppClient[jid].GenerateMessageID(),
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
		_, err = WhatsAppClient[jid].SendMessage(ctx, remoteJID, msgContent, msgExtra)
		if err != nil {
			return "", err
		}

		return msgExtra.ID, nil
	}

	// Return Error WhatsApp Client is not Valid
	return "", errors.New("WhatsApp Client is not Valid")
}

func WhatsAppSendLink(ctx context.Context, jid string, rjid string, linkCaption string, linkURL string) (string, error) {
	if WhatsAppClient[jid] != nil {
		var err error
		var urlTitle, urlDescription string

		// Make Sure WhatsApp Client is OK
		err = WhatsAppIsClientOK(jid)
		if err != nil {
			return "", err
		}

		// Make Sure WhatsApp ID is Registered
		remoteJID, err := WhatsAppCheckJID(jid, rjid)
		if err != nil {
			return "", err
		}

		// Set Chat Presence
		WhatsAppPresence(jid, true)
		WhatsAppComposeStatus(jid, remoteJID, true, false)
		defer func() {
			WhatsAppComposeStatus(jid, remoteJID, false, false)
			WhatsAppPresence(jid, false)
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
			ID: WhatsAppClient[jid].GenerateMessageID(),
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
		_, err = WhatsAppClient[jid].SendMessage(ctx, remoteJID, msgContent, msgExtra)
		if err != nil {
			return "", err
		}

		return msgExtra.ID, nil
	}

	// Return Error WhatsApp Client is not Valid
	return "", errors.New("WhatsApp Client is not Valid")
}

func WhatsAppSendSticker(ctx context.Context, jid string, rjid string, stickerBytes []byte) (string, error) {
	if WhatsAppClient[jid] != nil {
		var err error

		// Make Sure WhatsApp Client is OK
		err = WhatsAppIsClientOK(jid)
		if err != nil {
			return "", err
		}

		// Make Sure WhatsApp ID is Registered
		remoteJID, err := WhatsAppCheckJID(jid, rjid)
		if err != nil {
			return "", err
		}

		// Set Chat Presence
		WhatsAppPresence(jid, true)
		WhatsAppComposeStatus(jid, remoteJID, true, false)
		defer func() {
			WhatsAppComposeStatus(jid, remoteJID, false, false)
			WhatsAppPresence(jid, false)
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
		stickerUploaded, err := WhatsAppClient[jid].Upload(ctx, stickerBytes, whatsmeow.MediaImage)
		if err != nil {
			return "", errors.New("Error While Uploading Media to WhatsApp Server")
		}

		// Compose WhatsApp Proto
		msgExtra := whatsmeow.SendRequestExtra{
			ID: WhatsAppClient[jid].GenerateMessageID(),
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
		_, err = WhatsAppClient[jid].SendMessage(ctx, remoteJID, msgContent, msgExtra)
		if err != nil {
			return "", err
		}

		return msgExtra.ID, nil
	}

	// Return Error WhatsApp Client is not Valid
	return "", errors.New("WhatsApp Client is not Valid")
}

func WhatsAppSendPoll(ctx context.Context, jid string, rjid string, question string, options []string, isMultiAnswer bool) (string, error) {
	if WhatsAppClient[jid] != nil {
		var err error

		// Make Sure WhatsApp Client is OK
		err = WhatsAppIsClientOK(jid)
		if err != nil {
			return "", err
		}

		// Make Sure WhatsApp ID is Registered
		remoteJID, err := WhatsAppCheckJID(jid, rjid)
		if err != nil {
			return "", err
		}

		// Set Chat Presence
		WhatsAppPresence(jid, true)
		WhatsAppComposeStatus(jid, remoteJID, true, false)
		defer func() {
			WhatsAppComposeStatus(jid, remoteJID, false, false)
			WhatsAppPresence(jid, false)
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
			ID: WhatsAppClient[jid].GenerateMessageID(),
		}

		// Send WhatsApp Message Proto
		_, err = WhatsAppClient[jid].SendMessage(ctx, remoteJID, WhatsAppClient[jid].BuildPollCreation(question, options, pollAnswerMax), msgExtra)
		if err != nil {
			return "", err
		}

		return msgExtra.ID, nil
	}

	// Return Error WhatsApp Client is not Valid
	return "", errors.New("WhatsApp Client is not Valid")
}

func WhatsAppMessageEdit(ctx context.Context, jid string, rjid string, msgid string, message string) (string, error) {
	if WhatsAppClient[jid] != nil {
		var err error

		// Make Sure WhatsApp Client is OK
		err = WhatsAppIsClientOK(jid)
		if err != nil {
			return "", err
		}

		// Make Sure WhatsApp ID is Registered
		remoteJID, err := WhatsAppCheckJID(jid, rjid)
		if err != nil {
			return "", err
		}

		// Set Chat Presence
		WhatsAppPresence(jid, true)
		WhatsAppComposeStatus(jid, remoteJID, true, false)
		defer func() {
			WhatsAppComposeStatus(jid, remoteJID, false, false)
			WhatsAppPresence(jid, false)
		}()

		// Compose WhatsApp Proto
		msgContent := &waE2E.Message{
			Conversation: proto.String(message),
		}

		// Send WhatsApp Message Proto in Edit Mode
		_, err = WhatsAppClient[jid].SendMessage(ctx, remoteJID, WhatsAppClient[jid].BuildEdit(remoteJID, msgid, msgContent))
		if err != nil {
			return "", err
		}

		return msgid, nil
	}

	// Return Error WhatsApp Client is not Valid
	return "", errors.New("WhatsApp Client is not Valid")
}

func WhatsAppMessageReact(ctx context.Context, jid string, rjid string, msgid string, emoji string) (string, error) {
	if WhatsAppClient[jid] != nil {
		var err error

		// Make Sure WhatsApp Client is OK
		err = WhatsAppIsClientOK(jid)
		if err != nil {
			return "", err
		}

		// Make Sure WhatsApp ID is Registered
		remoteJID, err := WhatsAppCheckJID(jid, rjid)
		if err != nil {
			return "", err
		}

		// Set Chat Presence
		WhatsAppPresence(jid, true)
		WhatsAppComposeStatus(jid, remoteJID, true, false)
		defer func() {
			WhatsAppComposeStatus(jid, remoteJID, false, false)
			WhatsAppPresence(jid, false)
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
		_, err = WhatsAppClient[jid].SendMessage(ctx, remoteJID, msgReact)
		if err != nil {
			return "", err
		}

		return msgid, nil
	}

	// Return Error WhatsApp Client is not Valid
	return "", errors.New("WhatsApp Client is not Valid")

}

func WhatsAppMessageDelete(ctx context.Context, jid string, rjid string, msgid string) error {
	if WhatsAppClient[jid] != nil {
		var err error

		// Make Sure WhatsApp Client is OK
		err = WhatsAppIsClientOK(jid)
		if err != nil {
			return err
		}

		// Make Sure WhatsApp ID is Registered
		remoteJID, err := WhatsAppCheckJID(jid, rjid)
		if err != nil {
			return err
		}

		// Set Chat Presence
		WhatsAppPresence(jid, true)
		WhatsAppComposeStatus(jid, remoteJID, true, false)
		defer func() {
			WhatsAppComposeStatus(jid, remoteJID, false, false)
			WhatsAppPresence(jid, false)
		}()

		// Send WhatsApp Message Proto in Revoke Mode
		_, err = WhatsAppClient[jid].SendMessage(ctx, remoteJID, WhatsAppClient[jid].BuildRevoke(remoteJID, types.EmptyJID, msgid))
		if err != nil {
			return err
		}

		return nil
	}

	// Return Error WhatsApp Client is not Valid
	return errors.New("WhatsApp Client is not Valid")
}

func WhatsAppGroupGet(jid string) ([]types.GroupInfo, error) {
	if WhatsAppClient[jid] != nil {
		var err error

		// Make Sure WhatsApp Client is OK
		err = WhatsAppIsClientOK(jid)
		if err != nil {
			return nil, err
		}

		// Get Joined Group List
		groups, err := WhatsAppClient[jid].GetJoinedGroups()
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

func WhatsAppGroupJoin(jid string, link string) (string, error) {
	if WhatsAppClient[jid] != nil {
		var err error

		// Make Sure WhatsApp Client is OK
		err = WhatsAppIsClientOK(jid)
		if err != nil {
			return "", err
		}

		// Join Group By Invitation Link
		gid, err := WhatsAppClient[jid].JoinGroupWithLink(link)
		if err != nil {
			return "", err
		}

		// Return Joined Group ID
		return gid.String(), nil
	}

	// Return Error WhatsApp Client is not Valid
	return "", errors.New("WhatsApp Client is not Valid")
}

func WhatsAppGroupLeave(jid string, gjid string) error {
	if WhatsAppClient[jid] != nil {
		var err error

		// Make Sure WhatsApp Client is OK
		err = WhatsAppIsClientOK(jid)
		if err != nil {
			return err
		}

		// Make Sure WhatsApp ID is Registered
		groupJID, err := WhatsAppCheckJID(jid, gjid)
		if err != nil {
			return err
		}

		// Make Sure WhatsApp ID is Group Server
		if groupJID.Server != types.GroupServer {
			return errors.New("WhatsApp Group ID is Not Group Server")
		}

		// Leave Group By Group ID
		return WhatsAppClient[jid].LeaveGroup(groupJID)
	}

	// Return Error WhatsApp Client is not Valid
	return errors.New("WhatsApp Client is not Valid")
}

func initDB() error {
	var err error

	_, err = Db.Exec(`
		CREATE TABLE IF NOT EXISTS whatsmeow_clients (
		  id INTEGER PRIMARY KEY AUTOINCREMENT,
		  client_name TEXT NOT NULL,
		  uuid TEXT NOT NULL UNIQUE,
		  secret_key TEXT NOT NULL,
		  webhook_url TEXT NULL,
		  status_code INTEGER NOT NULL DEFAULT 1,
		  updated_at TEXT DEFAULT CURRENT_TIMESTAMP,
		  created_at TEXT DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_uuid ON whatsmeow_clients (uuid);

		CREATE TABLE IF NOT EXISTS whatsmeow_device_client_pivot (
		  id INTEGER PRIMARY KEY AUTOINCREMENT,
		  client_id INTEGER NOT NULL,
		  jid TEXT,
		  token TEXT NOT NULL UNIQUE,
		  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		  FOREIGN KEY (client_id) REFERENCES whatsmeow_clients(id) ON DELETE CASCADE
		);
	`)

	if err != nil {
		return fmt.Errorf("error creating table: %w", err)
	}

	return nil
}

func saveUUID(jid types.JID, token string) error {
	_, err := Db.Exec(`
		INSERT INTO whatsmeow_device_client_pivot (jid, token, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(token) DO UPDATE SET 
		token = excluded.token,
    	updated_at = excluded.updated_at;
		`,
		WhatsAppDecomposeJID(jid.User), token, time.Now(),
	)
	return err
}

func removeByUUID(token string) error {
	_, err := Db.Exec(`
  DELETE FROM whatsmeow_device_client_pivot
  WHERE token = ?
`, token)
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
	result, err := Db.Exec(`
        INSERT INTO whatsmeow_clients 
        (client_name, uuid, secret_key, webhook_url, status_code) 
        VALUES (?, ?, ?, ?, 1)`,
		req.Name, uuid, secretKey, req.WebhookURL,
	)
	if err != nil {
		fmt.Println(err)
		return router.ResponseInternalError(c, "Failed to create client")
	}

	// Get the inserted ID
	id, err := result.LastInsertId()
	if err != nil {
		return router.ResponseInternalError(c, "Failed to get client ID")
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
	return strings.TrimRight(keyWithoutUnderscores, "=")
}

// ClientStatusResponse defines the response structure
type ClientStatusResponse struct {
	ID         int64  `json:"id"`
	ClientName string `json:"client_name"`
	UUID       string `json:"uuid"`
	WebhookURL string `json:"webhook_url"`
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
        WHERE c.uuid = ?
        GROUP BY c.id`,
		uuid,
	).Scan(
		&response.ID,
		&response.ClientName,
		&response.UUID,
		&response.WebhookURL,
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
        SET status_code = ?, 
            updated_at = CURRENT_TIMESTAMP
        WHERE uuid = ?`,
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

	// Start transaction
	tx, err := Db.Begin()
	if err != nil {
		return router.ResponseInternalError(c, "Failed to start transaction")
	}

	// Delete from pivot table first (due to foreign key)
	_, err = tx.Exec(`
        DELETE FROM whatsmeow_device_client_pivot 
        WHERE client_id IN (
            SELECT id FROM whatsmeow_clients WHERE uuid = ?
        )`,
		uuid,
	)

	if err != nil {
		tx.Rollback()
		return router.ResponseInternalError(c, "Failed to delete client associations")
	}

	// Delete from clients table
	result, err := tx.Exec(`
        DELETE FROM whatsmeow_clients 
        WHERE uuid = ?`,
		uuid,
	)

	if err != nil {
		tx.Rollback()
		return router.ResponseInternalError(c, "Failed to delete client")
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		tx.Rollback()
		return router.ResponseNotFound(c, "Client not found")
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return router.ResponseInternalError(c, "Failed to complete deletion")
	}

	return router.ResponseSuccess(c, "Client deleted successfully")
}
