package bappservice

import (
	"bytes"
	"encoding/json"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"time"

	"fmt"
	"mime"
	"regexp"
	"strings"
	"sync"

	"github.com/gorilla/mux"

	gomatrix "maunium.net/go/mautrix"
	id "maunium.net/go/mautrix/id"

	"github.com/42wim/matterbridge/bridge"
	"github.com/42wim/matterbridge/bridge/config"
	"github.com/42wim/matterbridge/bridge/helper"
	matrix "github.com/matterbridge/gomatrix"
)

var (
	htmlTag            = regexp.MustCompile("</.*?>")
	htmlReplacementTag = regexp.MustCompile("<[^>]*>")
)

type NicknameCacheEntry struct {
	displayName string
	lastUpdated time.Time
}

type Bmatrix struct {
	mc          *matrix.Client
	UserID      string
	NicknameMap map[string]NicknameCacheEntry
	RoomMap     map[string]string
	rateMutex   sync.RWMutex
	sync.RWMutex
	*bridge.Config
}

type httpError struct {
	Errcode      string `json:"errcode"`
	Err          string `json:"error"`
	RetryAfterMs int    `json:"retry_after_ms"`
}

type matrixUsername struct {
	plain     string
	formatted string
}

// SubTextMessage represents the new content of the message in edit messages.
type SubTextMessage struct {
	MsgType       string `json:"msgtype"`
	Body          string `json:"body"`
	FormattedBody string `json:"formatted_body,omitempty"`
	Format        string `json:"format,omitempty"`
}

// MessageRelation explains how the current message relates to a previous message.
// Notably used for message edits.
type MessageRelation struct {
	EventID string `json:"event_id"`
	Type    string `json:"rel_type"`
}

type EditedMessage struct {
	NewContent SubTextMessage  `json:"m.new_content"`
	RelatedTo  MessageRelation `json:"m.relates_to"`
	matrix.TextMessage
}

type InReplyToRelationContent struct {
	EventID string `json:"event_id"`
}

type InReplyToRelation struct {
	InReplyTo InReplyToRelationContent `json:"m.in_reply_to"`
}

type ReplyMessage struct {
	RelatedTo InReplyToRelation `json:"m.relates_to"`
	matrix.TextMessage
}

type AppServMatrix struct {
	mc          *matrix.Client
	apsCli      *gomatrix.Client
	UserID      string
	NicknameMap map[string]NicknameCacheEntry
	RoomMap     map[string]string
	roomsInfo   map[string]MatrixRoomInfo
	rateMutex   sync.RWMutex
	sync.RWMutex
	*bridge.Config
}
type MatrixRoomInfo struct {
	RoomName string                `json:"room_name,omitempty"`
	Alias    string                `json:"alias,omitempty"`
	Members  map[string]MemberInfo `json:"members,omitempty"`
	IsDirect bool                  `json:"is_direct,omitempty"`
}
type MemberInfo struct {
	Token string
	Id    string
}

func (b *AppServMatrix) SaveState() {
	br, err := json.Marshal(b.roomsInfo)
	if err != nil {
		log.Println(br)
	}
	err = ioutil.WriteFile(b.GetString("StorePath"), br, 0666)  ////tmp/room-info.json
	if err != nil {
		log.Println(br)
		return
	}
}

func (b *AppServMatrix) LoadState() {
	br, err := ioutil.ReadFile(b.GetString("StorePath"))
	if err != nil {
		log.Println(br)
		return
	}
	err = json.Unmarshal(br, &(b.roomsInfo))
	if err != nil {
		log.Println(br)
	}
}

func (m MatrixRoomInfo) addNewMembers(newMembers map[string]MemberInfo) {
	for k, v := range newMembers {
		m.Members[k] = v
	}
}
func (m MatrixRoomInfo) GetMembersId() []string {
	membersID := []string{}
	for _, v := range m.Members {
		membersID = append(membersID, v.Id)
	}
	return membersID
}
func New(cfg *bridge.Config) bridge.Bridger {
	b := &AppServMatrix{Config: cfg}
	b.RoomMap = make(map[string]string)
	b.NicknameMap = make(map[string]NicknameCacheEntry)
	return b
}

func (b *AppServMatrix) Connect() error {
	mx := mux.NewRouter()
	mx.HandleFunc("/transactions/{txnId}", b.handleTransaction).Methods("PUT").Queries("access_token", "{token}")
	mx.HandleFunc("/users/{userId}", b.handleTransaction).Methods("GET").Queries("access_token", "{token}")
	mx.HandleFunc("/rooms/{roomAlias}", b.handleTransaction).Methods("GET").Queries("access_token", "{token}")

	var err error
//	b.apsCli, err = gomatrix.NewClient("http://localhost:8008", id.UserID("@_irc_bot:localhost"), "30c05ae90a248a4188e620216fa72e349803310ec83e2a77b34fe90be6081f46")
	b.apsCli, err = gomatrix.NewClient(b.GetString("Server"), id.UserID(b.GetString("MxID")), b.GetString("Token"))

	if err != nil {
		return err
	}
	go func() {
		log.Fatal(http.ListenAndServe(":1234", mx))
	}()
	return nil
}

type AppEvents struct {
	Events []*matrix.Event `json:"events,omitempty"`
}

func (b *AppServMatrix) handleTransaction(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	tx := vars["txnId"]

	log.Println(tx)
	tok := vars["token"]
	log.Println(tok)

	postBody, err := io.ReadAll(r.Body)
	if err != nil {
		log.Println(err)
		return
	}
	var appsEvents AppEvents
	err = json.Unmarshal(postBody, &appsEvents)
	log.Println(string(postBody))
	if err != nil {
		log.Println(err)
		return
	}
	for _, ev := range appsEvents.Events {
		b.HandleAppServEvent(ev)
	}
	// TODO
	//b.handlematrix()
	w.WriteHeader(http.StatusOK)
}
func (b *AppServMatrix) HandleAppServEvent(event *matrix.Event) {
	if event.Type == "m.room.member" {
		if val, ok := event.Content["membership"]; ok {
			if val == "invite" {

				b.HandleInvites(*event.StateKey, event.RoomID)
				// TODO handle invite
			}
		}
	}
	switch event.Type {
	case "m.room.redaction", "m.room.message":
		b.handleEvent(event)
	case "m.room.member":
		b.handleMemberChange(event)
	}

}
func (b *AppServMatrix) HandleInvites(userId, roomId string) error {
	if userId == "" {
		_, err := b.apsCli.JoinRoom(roomId, "", nil)
		if err != nil {
			return err
		}
		return nil
	}
	userName := strings.Split(userId[1:], ":")[0]
	userName = strings.ReplaceAll(userName, "_irc_bridge_", "")
	mc, errmtx := b.NewVirtualUserMtxClient(userName)
	if errmtx != nil {
		return errmtx
	}
	_, err := mc.JoinRoom(roomId, "", nil)
	if err != nil {
		return err
	}
	return nil

}
func (b *AppServMatrix) JoinRoom(roomId, userID, Token string) error {
	cli, err := gomatrix.NewClient(b.GetString("Server"), id.UserID(userID), Token)
	if err != nil {
		return err
	}
	_, err = cli.JoinRoom(roomId, "", nil)
	if err != nil {
		return err
	}
	return nil
}

func (b *AppServMatrix) Disconnect() error {
	return nil
}

func (b *AppServMatrix) JoinChannel(channel config.ChannelInfo) error {

	return nil

}

func (b *AppServMatrix) HandleNewUsers(channelUsers map[string][]string) error {

	// create Channel if not exist (direct or not)
	for channelName, members := range channelUsers {
		isDirect := false
		if  len(members) == 1 {
			isDirect = true
			channelName = members[0]
		}
		if b.isChannelExist(channelName) {
			newMembers := b.NewUsersInChannel(channelName, members)
			membersId := b.CreateVirtualUsers(channelName, newMembers)
			b.roomsInfo[channelName].addNewMembers(membersId)
			b.InviteToRoom(b.roomsInfo[channelName].Alias, b.roomsInfo[channelName].GetMembersId())

		}
		if !b.isChannelExist(channelName) {
			membersId := b.CreateVirtualUsers(channelName, members)
			var invitesId []string
			for _, v := range membersId {
				invitesId = append(invitesId, v.Id)
			}
			roomId, err := b.CreateRoom(channelName, invitesId, isDirect)
			if err != nil {
				return fmt.Errorf("failed to create room %s : %w", channelName, err)
			}
			b.roomsInfo[channelName] = MatrixRoomInfo{
				RoomName: channelName,
				Alias:    roomId,
				Members:  membersId,
				IsDirect: isDirect,
			}
			b.RoomMap[roomId] = channelName

			b.InviteToRoom(roomId, []string{"sampletest"})

		}
	}
	// create users if not exist
	// join users to channel
	return nil
}
func (b *AppServMatrix) isChannelExist(channelName string) bool {
	if _, ok := b.roomsInfo[channelName]; ok {
		return true
	}
	return false

}
func (b *AppServMatrix) NewUsersInChannel(channelName string, ExternMembers []string) []string {
	newMembers := []string{}
	roomInfo := b.roomsInfo[channelName]
	for _, ExternMember := range ExternMembers {
		exist := false
		for member, _ := range roomInfo.Members {
			if ExternMember == member {
				exist = true
				break
			}

		}
		if !exist {
			newMembers = append(newMembers, ExternMember)
		}
	}
	return newMembers
}
func (b *AppServMatrix) HandleDirectMessage(channelUsers map[string][]string) error {

	// TODO
	// verify if user exist ,
	// verify if room exist
	// create Channel if not exist (direct or not)
	/*
		for channelName, members := range channelUsers {
			isDirect := false
			if channelName == "mainUser" {
				isDirect = true
			}
			membersId := b.CreateVirtualUsers(channelName, members)
			roomId, err := b.CreateRoom(channelName, members, isDirect)
			if err != nil {
				return fmt.Errorf("failed to create room %s : %w", channelName, err)
			}
			b.InviteToRoom(roomId, []id.UserID{id.UserID("mainuser")})

		}
	*/
	// create users if not exist
	// join users to channel
	return nil
}
func (b *AppServMatrix) CreateVirtualUsers(channelName string, members []string) map[string]MemberInfo {
	membersId := make(map[string]MemberInfo, len(members))

	for _, member := range members {
		resp, _, err := b.apsCli.Register(&gomatrix.ReqRegister{
			Username: "_irc_bridge_" + member,
			Type:     "m.login.application_service",
		})
		if err != nil {
			log.Println(err)
			continue
		}
		membersId[member] = MemberInfo{resp.AccessToken, string(resp.UserID)}

	}
	return membersId
}
func (b *AppServMatrix) CreateRoom(roomName string, members []string, isDirect bool) (string, error) {
	preset := "public_chat"
	invites := []id.UserID{}
	for _, v := range members {
		invites = append(invites, id.UserID(v))
	}
	if isDirect {
		roomName = ""
		preset = "private_chat"
	}
	resp, err := b.apsCli.CreateRoom(&gomatrix.ReqCreateRoom{
		RoomAliasName: roomName,
		Name:          roomName,
		Topic:         "",
		Invite:        invites,
		Preset:        preset,
		IsDirect:      isDirect,
	})
	if err != nil {
		return "", err
	}
	return string(resp.RoomID), nil
}
func (b *AppServMatrix) InviteToRoom(roomId string, invites []string) {
	var invitesId []id.UserID
	for _, v := range invites {
		invitesId = append(invitesId, id.UserID(v))
	}
	for _, v := range invitesId {
		_, err := b.apsCli.InviteUser(id.RoomID(roomId), &gomatrix.ReqInviteUser{
			Reason: "",
			UserID: v,
		})
		if err != nil {
			log.Println(err)
		}
	}

}
func (b *AppServMatrix) isUserExist(channelName, userNick string) bool {

	roomInfo := b.roomsInfo[channelName]
	for member, _ := range roomInfo.Members {
		if userNick == member {
			return true
		}
	}
	return false

}
func (b *AppServMatrix) NewVirtualUserMtxClient(username string) (*matrix.Client, error) {
	var memberInfo MemberInfo
	for _, roomInfo := range b.roomsInfo {
		if val, ok := roomInfo.Members[username]; ok {
			memberInfo = val
			break
		}
	}
	return matrix.NewClient(b.GetString("Server"), memberInfo.Id, memberInfo.Token)

}

func (b *AppServMatrix) Send(msg config.Message) (string, error) {
	//	b.Log.Debugf("=> Receiving %#v", msg)

	//	b.Log.Debugf("Channel %s maps to channel id %s", msg.Channel, channel)
	// Make a action /me of the message

	//TODO handle virtualUser creation here
	if msg.Event == "new_users" {
		err := b.HandleNewUsers(msg.ChannelUsersMember)
		// TODO create virtual users and join channels
		return "", err
	}
	if msg.Event == "direct_msg" {

		err := b.HandleNewUsers(msg.ChannelUsersMember)
		if err != nil {
			return "", err
		}

		// TODO create virtual users and join channels
	}
	channel := b.getRoomID(msg.Channel)
	username := newMatrixUsername(msg.Username)

	mc, errmtx := b.NewVirtualUserMtxClient(msg.Username)
	if errmtx != nil {
		return "", errmtx
	}
	if msg.Event == config.EventUserAction {
		m := matrix.TextMessage{
			MsgType:       "m.emote",
			Body:          username.plain + msg.Text,
			FormattedBody: username.formatted + helper.ParseMarkdown(msg.Text),
			Format:        "org.matrix.custom.html",
		}

		if b.GetBool("HTMLDisable") {
			m.Format = ""
			m.FormattedBody = ""
		}

		msgID := ""

		err := b.retry(func() error {
			resp, err := mc.SendMessageEvent(channel, "m.room.message", m)
			if err != nil {
				return err
			}

			msgID = resp.EventID

			return err
		})

		return msgID, err
	}

	// Delete message
	if msg.Event == config.EventMsgDelete {
		if msg.ID == "" {
			return "", nil
		}

		msgID := ""

		err := b.retry(func() error {
			resp, err := mc.RedactEvent(channel, msg.ID, &matrix.ReqRedact{})
			if err != nil {
				return err
			}

			msgID = resp.EventID

			return err
		})

		return msgID, err
	}

	// Upload a file if it exists
	if msg.Extra != nil {
		for _, rmsg := range helper.HandleExtra(&msg, b.General) {
			rmsg := rmsg

			err := b.retry(func() error {
				_, err := mc.SendText(channel, rmsg.Username+rmsg.Text)

				return err
			})
			if err != nil {
				b.Log.Errorf("sendText failed: %s", err)
			}
		}
		// check if we have files to upload (from slack, telegram or mattermost)
		if len(msg.Extra["file"]) > 0 {
			return b.handleUploadFiles(&msg, channel)
		}
	}

	// Edit message if we have an ID
	if msg.ID != "" {
		rmsg := EditedMessage{
			TextMessage: matrix.TextMessage{
				Body:          username.plain + msg.Text,
				MsgType:       "m.text",
				Format:        "org.matrix.custom.html",
				FormattedBody: username.formatted + helper.ParseMarkdown(msg.Text),
			},
		}

		rmsg.NewContent = SubTextMessage{
			Body:          rmsg.TextMessage.Body,
			FormattedBody: rmsg.TextMessage.FormattedBody,
			Format:        rmsg.TextMessage.Format,
			MsgType:       "m.text",
		}

		if b.GetBool("HTMLDisable") {
			rmsg.TextMessage.Format = ""
			rmsg.TextMessage.FormattedBody = ""
			rmsg.NewContent.Format = ""
			rmsg.NewContent.FormattedBody = ""
		}

		rmsg.RelatedTo = MessageRelation{
			EventID: msg.ID,
			Type:    "m.replace",
		}

		err := b.retry(func() error {
			_, err := mc.SendMessageEvent(channel, "m.room.message", rmsg)

			return err
		})
		if err != nil {
			return "", err
		}

		return msg.ID, nil
	}

	// Use notices to send join/leave events
	if msg.Event == config.EventJoinLeave {
		m := matrix.TextMessage{
			MsgType:       "m.notice",
			Body:          username.plain + msg.Text,
			FormattedBody: username.formatted + msg.Text,
			Format:        "org.matrix.custom.html",
		}

		if b.GetBool("HTMLDisable") {
			m.Format = ""
			m.FormattedBody = ""
		}

		var (
			resp *matrix.RespSendEvent
			err  error
		)

		err = b.retry(func() error {
			resp, err = mc.SendMessageEvent(channel, "m.room.message", m)

			return err
		})
		if err != nil {
			return "", err
		}

		return resp.EventID, err
	}

	if msg.ParentValid() {
		m := ReplyMessage{
			TextMessage: matrix.TextMessage{
				MsgType:       "m.text",
				Body:          username.plain + msg.Text,
				FormattedBody: username.formatted + helper.ParseMarkdown(msg.Text),
				Format:        "org.matrix.custom.html",
			},
		}

		if b.GetBool("HTMLDisable") {
			m.TextMessage.Format = ""
			m.TextMessage.FormattedBody = ""
		}

		m.RelatedTo = InReplyToRelation{
			InReplyTo: InReplyToRelationContent{
				EventID: msg.ParentID,
			},
		}

		var (
			resp *matrix.RespSendEvent
			err  error
		)

		err = b.retry(func() error {
			resp, err = mc.SendMessageEvent(channel, "m.room.message", m)

			return err
		})
		if err != nil {
			return "", err
		}

		return resp.EventID, err
	}

	if b.GetBool("HTMLDisable") {
		var (
			resp *matrix.RespSendEvent
			err  error
		)

		err = b.retry(func() error {
			resp, err = mc.SendText(channel, username.plain+msg.Text)

			return err
		})
		if err != nil {
			return "", err
		}

		return resp.EventID, err
	}

	// Post normal message with HTML support (eg riot.im)
	var (
		resp *matrix.RespSendEvent
		err  error
	)

	err = b.retry(func() error {
		resp, err = mc.SendFormattedText(channel, username.plain+msg.Text,
			username.formatted+helper.ParseMarkdown(msg.Text))

		return err
	})
	if err != nil {
		return "", err
	}

	return resp.EventID, err
}

func (b *AppServMatrix) handlematrix() {
	syncer := b.mc.Syncer.(*matrix.DefaultSyncer)
	syncer.OnEventType("m.room.redaction", b.handleEvent)
	syncer.OnEventType("m.room.message", b.handleEvent)
	syncer.OnEventType("m.room.member", b.handleMemberChange)
	go func() {
		for {
			if b == nil {
				return
			}
			if err := b.mc.Sync(); err != nil {
				b.Log.Println("Sync() returned ", err)
			}
		}
	}()
}

func (b *AppServMatrix) handleEdit(ev *matrix.Event, rmsg config.Message) bool {
	relationInterface, present := ev.Content["m.relates_to"]
	newContentInterface, present2 := ev.Content["m.new_content"]
	if !(present && present2) {
		return false
	}

	var relation MessageRelation
	if err := interface2Struct(relationInterface, &relation); err != nil {
		b.Log.Warnf("Couldn't parse 'm.relates_to' object with value %#v", relationInterface)
		return false
	}

	var newContent SubTextMessage
	if err := interface2Struct(newContentInterface, &newContent); err != nil {
		b.Log.Warnf("Couldn't parse 'm.new_content' object with value %#v", newContentInterface)
		return false
	}

	if relation.Type != "m.replace" {
		return false
	}

	rmsg.ID = relation.EventID
	rmsg.Text = newContent.Body
	b.Remote <- rmsg

	return true
}

func (b *AppServMatrix) handleReply(ev *matrix.Event, rmsg config.Message) bool {
	relationInterface, present := ev.Content["m.relates_to"]
	if !present {
		return false
	}

	var relation InReplyToRelation
	if err := interface2Struct(relationInterface, &relation); err != nil {
		// probably fine
		return false
	}

	body := rmsg.Text

	if !b.GetBool("keepquotedreply") {
		for strings.HasPrefix(body, "> ") {
			lineIdx := strings.IndexRune(body, '\n')
			if lineIdx == -1 {
				body = ""
			} else {
				body = body[(lineIdx + 1):]
			}
		}
	}

	rmsg.Text = body
	rmsg.ParentID = relation.InReplyTo.EventID
	b.Remote <- rmsg

	return true
}

func (b *AppServMatrix) handleMemberChange(ev *matrix.Event) {
	// Update the displayname on join messages, according to https://matrix.org/docs/spec/client_server/r0.6.1#events-on-change-of-profile-information
	if ev.Content["membership"] == "join" {
		if dn, ok := ev.Content["displayname"].(string); ok {
			b.cacheDisplayName(ev.Sender, dn)
		}
	}
}

func (b *AppServMatrix) handleEvent(ev *matrix.Event) {
	b.Log.Debugf("== Receiving event: %#v", ev)
	if ev.Sender != b.UserID {
		b.RLock()
		channel, ok := b.RoomMap[ev.RoomID]
		b.RUnlock()
		if !ok {
			b.Log.Debugf("Unknown room %s", ev.RoomID)
			return
		}

		// Create our message
		rmsg := config.Message{
			Username: b.getDisplayName(ev.Sender),
			Channel:  channel,
			Account:  b.Account,
			UserID:   ev.Sender,
			ID:       ev.ID,
			Avatar:   b.getAvatarURL(ev.Sender),
		}

		// Remove homeserver suffix if configured
		if b.GetBool("NoHomeServerSuffix") {
			re := regexp.MustCompile("(.*?):.*")
			rmsg.Username = re.ReplaceAllString(rmsg.Username, `$1`)
		}

		// Delete event
		if ev.Type == "m.room.redaction" {
			rmsg.Event = config.EventMsgDelete
			rmsg.ID = ev.Redacts
			rmsg.Text = config.EventMsgDelete
			b.Remote <- rmsg
			return
		}

		// Text must be a string
		if rmsg.Text, ok = ev.Content["body"].(string); !ok {
			b.Log.Errorf("Content[body] is not a string: %T\n%#v",
				ev.Content["body"], ev.Content)
			return
		}

		// Do we have a /me action
		if ev.Content["msgtype"].(string) == "m.emote" {
			rmsg.Event = config.EventUserAction
		}

		// Is it an edit?
		if b.handleEdit(ev, rmsg) {
			return
		}

		// Is it a reply?
		if b.handleReply(ev, rmsg) {
			return
		}

		// Do we have attachments
		if b.containsAttachment(ev.Content) {
			err := b.handleDownloadFile(&rmsg, ev.Content)
			if err != nil {
				b.Log.Errorf("download failed: %#v", err)
			}
		}

		b.Log.Debugf("<= Sending message from %s on %s to gateway", ev.Sender, b.Account)
		b.Remote <- rmsg

		// not crucial, so no ratelimit check here
		if err := b.apsCli.MarkRead(id.RoomID(ev.RoomID), id.EventID(ev.ID)); err != nil {
			b.Log.Errorf("couldn't mark message as read %s", err.Error())
		}
	}
}

// handleDownloadFile handles file download
func (b *AppServMatrix) handleDownloadFile(rmsg *config.Message, content map[string]interface{}) error {
	var (
		ok                        bool
		url, name, msgtype, mtype string
		info                      map[string]interface{}
		size                      float64
	)

	rmsg.Extra = make(map[string][]interface{})
	if url, ok = content["url"].(string); !ok {
		return fmt.Errorf("url isn't a %T", url)
	}
	url = strings.Replace(url, "mxc://", b.GetString("Server")+"/_matrix/media/v1/download/", -1)

	if info, ok = content["info"].(map[string]interface{}); !ok {
		return fmt.Errorf("info isn't a %T", info)
	}
	if size, ok = info["size"].(float64); !ok {
		return fmt.Errorf("size isn't a %T", size)
	}
	if name, ok = content["body"].(string); !ok {
		return fmt.Errorf("name isn't a %T", name)
	}
	if msgtype, ok = content["msgtype"].(string); !ok {
		return fmt.Errorf("msgtype isn't a %T", msgtype)
	}
	if mtype, ok = info["mimetype"].(string); !ok {
		return fmt.Errorf("mtype isn't a %T", mtype)
	}

	// check if we have an image uploaded without extension
	if !strings.Contains(name, ".") {
		if msgtype == "m.image" {
			mext, _ := mime.ExtensionsByType(mtype)
			if len(mext) > 0 {
				name += mext[0]
			}
		} else {
			// just a default .png extension if we don't have mime info
			name += ".png"
		}
	}

	// check if the size is ok
	err := helper.HandleDownloadSize(b.Log, rmsg, name, int64(size), b.General)
	if err != nil {
		return err
	}
	// actually download the file
	data, err := helper.DownloadFile(url)
	if err != nil {
		return fmt.Errorf("download %s failed %#v", url, err)
	}
	// add the downloaded data to the message
	helper.HandleDownloadData(b.Log, rmsg, name, "", url, data, b.General)
	return nil
}

// handleUploadFiles handles native upload of files.
func (b *AppServMatrix) handleUploadFiles(msg *config.Message, channel string) (string, error) {
	for _, f := range msg.Extra["file"] {
		if fi, ok := f.(config.FileInfo); ok {
			b.handleUploadFile(msg, channel, &fi)
		}
	}
	return "", nil
}

// handleUploadFile handles native upload of a file.
func (b *AppServMatrix) handleUploadFile(msg *config.Message, channel string, fi *config.FileInfo) {
	mc, errmtx := b.NewVirtualUserMtxClient(msg.Username)
	if errmtx != nil {
		b.Log.Errorf("couldn't mark message as read %s", errmtx.Error())
	}
	username := newMatrixUsername(msg.Username)
	content := bytes.NewReader(*fi.Data)
	sp := strings.Split(fi.Name, ".")
	mtype := mime.TypeByExtension("." + sp[len(sp)-1])
	// image and video uploads send no username, we have to do this ourself here #715
	err := b.retry(func() error {
		_, err := mc.SendFormattedText(channel, username.plain+fi.Comment, username.formatted+fi.Comment)

		return err
	})
	if err != nil {
		b.Log.Errorf("file comment failed: %#v", err)
	}

	b.Log.Debugf("uploading file: %s %s", fi.Name, mtype)

	var res *matrix.RespMediaUpload

	err = b.retry(func() error {
		res, err = mc.UploadToContentRepo(content, mtype, int64(len(*fi.Data)))

		return err
	})

	if err != nil {
		b.Log.Errorf("file upload failed: %#v", err)
		return
	}

	switch {
	case strings.Contains(mtype, "video"):
		b.Log.Debugf("sendVideo %s", res.ContentURI)
		err = b.retry(func() error {
			_, err = mc.SendVideo(channel, fi.Name, res.ContentURI)

			return err
		})
		if err != nil {
			b.Log.Errorf("sendVideo failed: %#v", err)
		}
	case strings.Contains(mtype, "image"):
		b.Log.Debugf("sendImage %s", res.ContentURI)
		err = b.retry(func() error {
			_, err = mc.SendImage(channel, fi.Name, res.ContentURI)

			return err
		})
		if err != nil {
			b.Log.Errorf("sendImage failed: %#v", err)
		}
	case strings.Contains(mtype, "audio"):
		b.Log.Debugf("sendAudio %s", res.ContentURI)
		err = b.retry(func() error {
			_, err = mc.SendMessageEvent(channel, "m.room.message", matrix.AudioMessage{
				MsgType: "m.audio",
				Body:    fi.Name,
				URL:     res.ContentURI,
				Info: matrix.AudioInfo{
					Mimetype: mtype,
					Size:     uint(len(*fi.Data)),
				},
			})

			return err
		})
		if err != nil {
			b.Log.Errorf("sendAudio failed: %#v", err)
		}
	default:
		b.Log.Debugf("sendFile %s", res.ContentURI)
		err = b.retry(func() error {
			_, err = mc.SendMessageEvent(channel, "m.room.message", matrix.FileMessage{
				MsgType: "m.file",
				Body:    fi.Name,
				URL:     res.ContentURI,
				Info: matrix.FileInfo{
					Mimetype: mtype,
					Size:     uint(len(*fi.Data)),
				},
			})

			return err
		})
		if err != nil {
			b.Log.Errorf("sendFile failed: %#v", err)
		}
	}
	b.Log.Debugf("result: %#v", res)
}