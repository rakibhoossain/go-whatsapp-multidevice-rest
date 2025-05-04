package internal

import (
	"github.com/labstack/echo/v4"
	"github.com/rakibhoossain/go-whatsapp-multidevice-rest/docs"
	eSwagger "github.com/swaggo/echo-swagger"

	"github.com/rakibhoossain/go-whatsapp-multidevice-rest/pkg/auth"
	"github.com/rakibhoossain/go-whatsapp-multidevice-rest/pkg/router"

	ctlIndex "github.com/rakibhoossain/go-whatsapp-multidevice-rest/internal/index"
	ctlWhatsApp "github.com/rakibhoossain/go-whatsapp-multidevice-rest/internal/whatsapp"
)

func Routes(e *echo.Echo) {
	// Configure OpenAPI / Swagger
	docs.SwaggerInfo.BasePath = router.BaseURL

	// Route for Index
	// ---------------------------------------------
	e.GET(router.BaseURL, ctlIndex.Index)
	e.GET(router.BaseURL+"/", ctlIndex.Index)

	// Route for OpenAPI / Swagger
	// ---------------------------------------------
	e.GET(router.BaseURL+"/docs/*", eSwagger.WrapHandler)

	// Route for WhatsApp
	// ---------------------------------------------
	e.POST(router.BaseURL+"/client", ctlWhatsApp.CreateClient, auth.BasicAdminAuth())
	e.GET(router.BaseURL+"/client/:uuid", ctlWhatsApp.ClientStatus, auth.BasicAdminAuth())
	e.POST(router.BaseURL+"/client/:uuid", ctlWhatsApp.ClientStatusEdit, auth.BasicAdminAuth())
	e.DELETE(router.BaseURL+"/client/:uuid", ctlWhatsApp.ClientDelete, auth.BasicAdminAuth())

	e.POST(router.BaseURL+"/login", ctlWhatsApp.Login, auth.BasicAuth())
	e.POST(router.BaseURL+"/login/pair", ctlWhatsApp.LoginPair, auth.BasicAuth())
	e.POST(router.BaseURL+"/logout", ctlWhatsApp.Logout, auth.BasicAuth())
	e.GET(router.BaseURL+"/status", ctlWhatsApp.Status, auth.BasicAuth())

	e.GET(router.BaseURL+"/ws", ctlWhatsApp.WebsocketConnect, auth.WebsocketAuth())

	e.GET(router.BaseURL+"/registered", ctlWhatsApp.Registered, auth.BasicAuth())

	e.GET(router.BaseURL+"/group", ctlWhatsApp.GetGroup, auth.BasicAuth())
	e.POST(router.BaseURL+"/group/join", ctlWhatsApp.JoinGroup, auth.BasicAuth())
	e.POST(router.BaseURL+"/group/leave", ctlWhatsApp.LeaveGroup, auth.BasicAuth())

	e.POST(router.BaseURL+"/send/text", ctlWhatsApp.SendText, auth.BasicAuth())
	e.POST(router.BaseURL+"/send/location", ctlWhatsApp.SendLocation, auth.BasicAuth())
	e.POST(router.BaseURL+"/send/contact", ctlWhatsApp.SendContact, auth.BasicAuth())
	e.POST(router.BaseURL+"/send/link", ctlWhatsApp.SendLink, auth.BasicAuth())
	e.POST(router.BaseURL+"/send/document", ctlWhatsApp.SendDocument, auth.BasicAuth())
	e.POST(router.BaseURL+"/send/image", ctlWhatsApp.SendImage, auth.BasicAuth())
	e.POST(router.BaseURL+"/send/audio", ctlWhatsApp.SendAudio, auth.BasicAuth())
	e.POST(router.BaseURL+"/send/video", ctlWhatsApp.SendVideo, auth.BasicAuth())
	e.POST(router.BaseURL+"/send/sticker", ctlWhatsApp.SendSticker, auth.BasicAuth())
	e.POST(router.BaseURL+"/send/poll", ctlWhatsApp.SendPoll, auth.BasicAuth())

	e.POST(router.BaseURL+"/message/edit", ctlWhatsApp.MessageEdit, auth.BasicAuth())
	e.POST(router.BaseURL+"/message/react", ctlWhatsApp.MessageEdit, auth.BasicAuth())
	e.POST(router.BaseURL+"/message/delete", ctlWhatsApp.MessageDelete, auth.BasicAuth())
}
