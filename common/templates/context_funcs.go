package templates

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jonas747/discordgo"
	"github.com/jonas747/dstate/v3"
	"github.com/jonas747/yagpdb/bot"
	"github.com/jonas747/yagpdb/common"
	"github.com/jonas747/yagpdb/common/scheduledevents2"
)

var ErrTooManyCalls = errors.New("too many calls to this function")
var ErrTooManyAPICalls = errors.New("too many potential discord api calls function")

func (c *Context) tmplSendDM(s ...interface{}) string {
	if len(s) < 1 || c.IncreaseCheckCallCounter("send_dm", 1) || c.MS == nil {
		return ""
	}

	gIcon := discordgo.EndpointGuildIcon(c.GS.ID, c.GS.Icon)

	info := fmt.Sprintf("Custom Command DM from the server **%s**", c.GS.Name)
	embedInfo := fmt.Sprintf("Custom Command DM from the server %s", c.GS.Name)
	msgSend := &discordgo.MessageSend{
		AllowedMentions: discordgo.AllowedMentions{
			Parse: []discordgo.AllowedMentionType{discordgo.AllowedMentionTypeUsers},
		},
	}

	switch t := s[0].(type) {
	case *discordgo.MessageEmbed:
		t.Footer = &discordgo.MessageEmbedFooter{
			Text:    embedInfo,
			IconURL: gIcon,
		}
		msgSend.Embed = t
	case *discordgo.MessageSend:
		msgSend = t
		if msgSend.Embed != nil {
			msgSend.Embed.Footer = &discordgo.MessageEmbedFooter{
				Text:    embedInfo,
				IconURL: gIcon,
			}
			break
		}
		if (strings.TrimSpace(msgSend.Content) == "") && (msgSend.File == nil) {
			return ""
		}
		msgSend.Content = info + "\n" + msgSend.Content
	default:
		msgSend.Content = fmt.Sprintf("%s\n%s", info, fmt.Sprint(s...))
	}

	channel, err := common.BotSession.UserChannelCreate(c.MS.User.ID)
	if err != nil {
		return ""
	}
	_, _ = common.BotSession.ChannelMessageSendComplex(channel.ID, msgSend)
	return ""
}

// ChannelArg converts a verity of types of argument into a channel, verifying that it exists
func (c *Context) ChannelArg(v interface{}) int64 {

	// Look for the channel
	if v == nil && c.CurrentFrame.CS != nil {
		// No channel passed, assume current channel
		return c.CurrentFrame.CS.ID
	}

	verifiedExistence := false
	var cid int64
	if v != nil {
		switch t := v.(type) {
		case int, int64:
			// Channel id passed
			cid = ToInt64(t)
		case string:
			parsed, err := strconv.ParseInt(t, 10, 64)
			if err == nil {
				// Channel id passed in string format
				cid = parsed
			} else {
				// Channel name, look for it
				for _, v := range c.GS.Channels {
					if strings.EqualFold(t, v.Name) && v.Type == discordgo.ChannelTypeGuildText {
						cid = v.ID
						verifiedExistence = true
						break
					}
				}
			}
		}
	}

	if !verifiedExistence {
		// Make sure the channel is part of the guild
		if channel := c.GS.GetChannel(cid); channel != nil {
			verifiedExistence = true
		}
	}

	if !verifiedExistence {
		return 0
	}

	return cid
}

// ChannelArgNoDM is the same as ChannelArg but will not accept DM channels
func (c *Context) ChannelArgNoDM(v interface{}) int64 {

	// Look for the channel
	if v == nil && c.CurrentFrame.CS != nil {
		// No channel passed, assume current channel
		v = c.CurrentFrame.CS.ID
	}

	verifiedExistence := false
	var cid int64
	if v != nil {
		switch t := v.(type) {
		case int, int64:
			// Channel id passed
			cid = ToInt64(t)
		case string:
			parsed, err := strconv.ParseInt(t, 10, 64)
			if err == nil {
				// Channel id passed in string format
				cid = parsed
			} else {
				// Channel name, look for it
				for _, v := range c.GS.Channels {
					if strings.EqualFold(t, v.Name) && v.Type == discordgo.ChannelTypeGuildText {
						cid = v.ID
						verifiedExistence = true
						break
					}
				}
			}
		}
	}

	if !verifiedExistence {
		// Make sure the channel is part of the guild
		if channel := c.GS.GetChannel(cid); channel != nil {
			verifiedExistence = true
		}
	}

	if !verifiedExistence {
		return 0
	}

	return cid
}

func (c *Context) tmplSendTemplateDM(name string, data ...interface{}) (interface{}, error) {
	return c.sendNestedTemplate(nil, true, name, data...)
}

func (c *Context) tmplSendTemplate(channel interface{}, name string, data ...interface{}) (interface{}, error) {
	return c.sendNestedTemplate(channel, false, name, data...)
}

func (c *Context) sendNestedTemplate(channel interface{}, dm bool, name string, data ...interface{}) (interface{}, error) {
	if c.IncreaseCheckCallCounter("exec_child", 3) {
		return "", ErrTooManyCalls
	}
	if name == "" {
		return "", errors.New("no template name passed")
	}
	if c.CurrentFrame.isNestedTemplate {
		return "", errors.New("can't call this in a nested template")
	}

	t := c.CurrentFrame.parsedTemplate.Lookup(name)
	if t == nil {
		return "", errors.New("unknown template")
	}

	var cs *dstate.ChannelState
	// find the new context channel
	if !dm {
		if channel == nil {
			cs = c.CurrentFrame.CS
		} else {
			cID := c.ChannelArg(channel)
			if cID == 0 {
				return "", errors.New("unknown channel")
			}

			cs = c.GS.GetChannel(cID)
			if cs == nil {
				return "", errors.New("unknown channel")
			}
		}
	} else {
		if c.CurrentFrame.SendResponseInDM {
			cs = c.CurrentFrame.CS
		} else {
			ch, err := common.BotSession.UserChannelCreate(c.MS.User.ID)
			if err != nil {
				return "", err
			}

			cs = &dstate.ChannelState{
				GuildID: c.GS.ID,
				ID:      ch.ID,
				Name:    c.MS.User.Username,
				Type:    discordgo.ChannelTypeDM,
			}
		}
	}

	oldFrame := c.newContextFrame(cs)
	defer func() {
		c.CurrentFrame = oldFrame
	}()

	if dm {
		c.CurrentFrame.SendResponseInDM = oldFrame.SendResponseInDM
	} else if channel == nil {
		// inherit
		c.CurrentFrame.SendResponseInDM = oldFrame.SendResponseInDM
	}

	// pass some data
	if len(data) > 1 {
		dict, _ := Dictionary(data...)
		c.Data["TemplateArgs"] = dict
		if !c.checkSafeDictNoRecursion(dict, 0) {
			return nil, errors.New("trying to pass the entire current context data in as templateargs, this is not needed, just use nil and access all other data normally")
		}
	} else if len(data) == 1 {
		if cast, ok := data[0].(map[string]interface{}); ok && reflect.DeepEqual(cast, c.Data) {
			return nil, errors.New("trying to pass the entire current context data in as templateargs, this is not needed, just use nil and access all other data normally")
		}
		c.Data["TemplateArgs"] = data[0]
	}

	// and finally execute the child template
	c.CurrentFrame.parsedTemplate = t
	resp, err := c.executeParsed()
	if err != nil {
		return "", err
	}

	m, err := c.SendResponse(resp)
	if err != nil {
		return "", err
	}

	if m != nil {
		return m.ID, err
	}
	return "", err
}

func (c *Context) checkSafeStringDictNoRecursion(d SDict, n int) bool {
	if n > 1000 {
		return false
	}

	for _, v := range d {
		if cast, ok := v.(Dict); ok {
			if !c.checkSafeDictNoRecursion(cast, n+1) {
				return false
			}
		}

		if cast, ok := v.(SDict); ok {
			if !c.checkSafeStringDictNoRecursion(cast, n+1) {
				return false
			}
		}

		if reflect.DeepEqual(v, c.Data) {
			return false
		}
	}

	return true
}

func (c *Context) checkSafeDictNoRecursion(d Dict, n int) bool {
	if n > 1000 {
		return false
	}

	for _, v := range d {
		if cast, ok := v.(Dict); ok {
			if !c.checkSafeDictNoRecursion(cast, n+1) {
				return false
			}
		}

		if cast, ok := v.(SDict); ok {
			if !c.checkSafeStringDictNoRecursion(cast, n+1) {
				return false
			}
		}

		if reflect.DeepEqual(v, c.Data) {
			return false
		}
	}

	return true
}

func (c *Context) tmplSendMessage(filterSpecialMentions bool, returnID bool) func(channel interface{}, msg interface{}) interface{} {
	parseMentions := []discordgo.AllowedMentionType{discordgo.AllowedMentionTypeUsers}
	if !filterSpecialMentions {
		parseMentions = append(parseMentions, discordgo.AllowedMentionTypeRoles, discordgo.AllowedMentionTypeEveryone)
	}

	return func(channel interface{}, msg interface{}) interface{} {
		if c.IncreaseCheckGenericAPICall() {
			return ""
		}

		cid := c.ChannelArg(channel)
		if cid == 0 {
			return ""
		}

		var m *discordgo.Message
		msgSend := &discordgo.MessageSend{
			AllowedMentions: discordgo.AllowedMentions{
				Parse: parseMentions,
			},
		}
		var err error

		switch typedMsg := msg.(type) {

		case *discordgo.MessageEmbed:
			msgSend.Embed = typedMsg
		case *discordgo.MessageSend:
			msgSend = typedMsg
			msgSend.AllowedMentions = discordgo.AllowedMentions{
				Parse: parseMentions,
			}
		default:
			msgSend.Content = fmt.Sprint(msg)
		}

		m, err = common.BotSession.ChannelMessageSendComplex(cid, msgSend)

		if err == nil && returnID {
			return m.ID
		}

		return ""
	}
}

func (c *Context) tmplEditMessage(filterSpecialMentions bool) func(channel interface{}, msgID interface{}, msg interface{}) (interface{}, error) {
	return func(channel interface{}, msgID interface{}, msg interface{}) (interface{}, error) {
		if c.IncreaseCheckGenericAPICall() {
			return "", ErrTooManyAPICalls
		}

		cid := c.ChannelArgNoDM(channel)
		if cid == 0 {
			return "", errors.New("unknown channel")
		}

		mID := ToInt64(msgID)
		msgEdit := &discordgo.MessageEdit{
			ID:      mID,
			Channel: cid,
		}
		var err error

		switch typedMsg := msg.(type) {

		case *discordgo.MessageEmbed:
			msgEdit.Embed = typedMsg
		case *discordgo.MessageEdit:
			//If both Embed and string are explicitly set as null, give an error message.
			if typedMsg.Content != nil && strings.TrimSpace(*typedMsg.Content) == "" && typedMsg.Embed != nil && typedMsg.Embed.GetMarshalNil() {
				return "", errors.New("both content and embed cannot be null")
			}
			msgEdit.Content = typedMsg.Content
			msgEdit.Embed = typedMsg.Embed
		default:
			temp := fmt.Sprint(msg)
			msgEdit.Content = &temp
		}

		_, err = common.BotSession.ChannelMessageEditComplex(msgEdit)

		if err != nil {
			return "", err
		}

		return "", nil
	}
}

func (c *Context) tmplMentionEveryone() string {
	c.CurrentFrame.MentionEveryone = true
	return "@everyone"
}

func (c *Context) tmplMentionHere() string {
	c.CurrentFrame.MentionHere = true
	return "@here"
}

func (c *Context) tmplMentionRoleID(roleID interface{}) string {
	if c.IncreaseCheckStateLock() {
		return ""
	}

	var role int64
	switch r := roleID.(type) {
	case int64:
		role = r
	case int:
		role = int64(r)
	case string:
		role, _ = strconv.ParseInt(r, 10, 64)
	default:
		return ""
	}

	r := c.GS.GetRole(role)
	if r == nil {
		return "(role not found)"
	}

	if common.ContainsInt64Slice(c.CurrentFrame.MentionRoles, role) {
		return "<@&" + discordgo.StrID(role) + ">"
	}

	c.CurrentFrame.MentionRoles = append(c.CurrentFrame.MentionRoles, role)
	return "<@&" + discordgo.StrID(role) + ">"
}

func (c *Context) tmplMentionRoleName(role string) string {
	if c.IncreaseCheckStateLock() {
		return ""
	}

	var found *discordgo.Role
	for _, r := range c.GS.Roles {
		if r.Name == role {
			if !common.ContainsInt64Slice(c.CurrentFrame.MentionRoles, r.ID) {
				c.CurrentFrame.MentionRoles = append(c.CurrentFrame.MentionRoles, r.ID)

				// make a copy as the looping var is changing
				cop := r
				found = &cop
			}
		}
	}
	if found == nil {
		return "(role not found)"
	}

	return "<@&" + discordgo.StrID(found.ID) + ">"
}

func (c *Context) tmplHasRoleID(roleID interface{}) bool {
	role := ToInt64(roleID)
	if role == 0 {
		return false
	}

	contains := common.ContainsInt64Slice(c.MS.Member.Roles, role)
	return contains
}

func (c *Context) tmplHasRoleName(name string) (bool, error) {
	if c.IncreaseCheckStateLock() {
		return false, ErrTooManyCalls
	}

	for _, r := range c.GS.Roles {
		if strings.EqualFold(r.Name, name) {
			if common.ContainsInt64Slice(c.MS.Member.Roles, r.ID) {
				return true, nil
			}

			return false, nil

		}
	}

	// Role not found, default to false
	return false, nil
}

func targetUserID(input interface{}) int64 {
	switch t := input.(type) {
	case *discordgo.User:
		return t.ID
	case string:
		str := strings.TrimSpace(t)
		if strings.HasPrefix(str, "<@") && strings.HasSuffix(str, ">") && (len(str) > 4) {
			trimmed := str[2 : len(str)-1]
			if trimmed[0] == '!' {
				trimmed = trimmed[1:]
			}
			str = trimmed
		}

		return ToInt64(str)
	default:
		return ToInt64(input)
	}
}

func (c *Context) tmplTargetHasRoleID(target interface{}, roleID interface{}) bool {
	if c.IncreaseCheckStateLock() {
		return false
	}

	targetID := targetUserID(target)
	if targetID == 0 {
		return false
	}

	ts, err := bot.GetMember(c.GS.ID, targetID)
	if err != nil {
		return false
	}

	role := ToInt64(roleID)
	if role == 0 {
		return false
	}

	contains := common.ContainsInt64Slice(ts.Member.Roles, role)

	return contains

}

func (c *Context) tmplTargetHasRoleName(target interface{}, name string) bool {
	if c.IncreaseCheckStateLock() {
		return false
	}

	targetID := targetUserID(target)
	if targetID == 0 {
		return false
	}

	ts, err := bot.GetMember(c.GS.ID, targetID)
	if err != nil {
		return false
	}

	for _, r := range c.GS.Roles {
		if strings.EqualFold(r.Name, name) {
			return common.ContainsInt64Slice(ts.Member.Roles, r.ID)
		}
	}

	return false

}

func (c *Context) tmplGiveRoleID(target interface{}, roleID interface{}) string {
	if c.IncreaseCheckGenericAPICall() {
		return ""
	}

	targetID := targetUserID(target)
	if targetID == 0 {
		return ""
	}

	role := ToInt64(roleID)
	if role == 0 {
		return ""
	}

	return c.giveRole(targetID, role)
}

func (c *Context) tmplGiveRoleName(target interface{}, name string) string {
	if c.IncreaseCheckGenericAPICall() {
		return ""
	}

	targetID := targetUserID(target)
	if targetID == 0 {
		return ""
	}

	role := c.findRoleByName(name)
	if role == nil {
		return "no role by the name of " + name + " found"
	}

	return c.giveRole(targetID, role.ID)
}

func (c *Context) giveRole(targetID int64, roleID int64) string {
	if c.GS.GetRole(roleID) == nil {
		return "" // role does not exist
	}

	// Check to see if we can save a API request here
	ms, err := bot.GetMember(c.GS.ID, targetID)
	if err != nil {
		return ""
	}

	if !common.ContainsInt64Slice(ms.Member.Roles, roleID) {
		common.BotSession.GuildMemberRoleAdd(c.GS.ID, targetID, roleID)
	}

	return ""
}

func (c *Context) tmplTakeRoleID(target interface{}, roleID interface{}, optionalArgs ...interface{}) string {
	if c.IncreaseCheckGenericAPICall() {
		return ""
	}

	delay := 0
	if len(optionalArgs) > 0 {
		delay = tmplToInt(optionalArgs[0])
	}

	targetID := targetUserID(target)
	if targetID == 0 {
		return ""
	}

	role := ToInt64(roleID)
	if role == 0 {
		return ""
	}

	return c.takeRole(targetID, role, time.Second*time.Duration(delay))
}

func (c *Context) tmplTakeRoleName(target interface{}, name string, optionalArgs ...interface{}) string {
	if c.IncreaseCheckGenericAPICall() {
		return ""
	}

	delay := 0
	if len(optionalArgs) > 0 {
		delay = tmplToInt(optionalArgs[0])
	}

	targetID := targetUserID(target)
	if targetID == 0 {
		return ""
	}

	role := c.findRoleByName(name)
	if role != nil {
		return c.takeRole(targetID, role.ID, time.Second*time.Duration(delay))
	}

	return ""
}

func (c *Context) takeRole(targetID int64, roleID int64, delay time.Duration) string {
	if c.GS.GetRole(roleID) == nil {
		return "" // role does not exist
	}

	ms, err := bot.GetMember(c.GS.ID, targetID)
	if err != nil {
		return ""
	}

	if !common.ContainsInt64Slice(ms.Member.Roles, roleID) {
		return ""
	}

	if delay > 0 {
		scheduledevents2.ScheduleRemoveRole(context.Background(), c.GS.ID, targetID, roleID, time.Now().Add(delay))
	} else {
		common.BotSession.GuildMemberRoleRemove(c.GS.ID, targetID, roleID)
	}

	return ""
}

func (c *Context) tmplSetRoles(target interface{}, roleSlice interface{}) (string, error) {
	if c.IncreaseCheckGenericAPICall() {
		return "", ErrTooManyAPICalls
	}

	targetID := targetUserID(target)
	if targetID == 0 {
		return "", nil
	}

	if c.IncreaseCheckCallCounter("set_roles"+discordgo.StrID(targetID), 1) {
		return "", errors.New("Too many calls to setRoles for specific user ID (max 1 / user)")
	}

	rSlice := reflect.ValueOf(roleSlice)
	switch rSlice.Kind() {
	case reflect.Slice, reflect.Array:
		// ok
	default:
		return "", errors.New("Value passed was not an array or slice")
	}

	if rSlice.Len() > 250 {
		return "", errors.New("Length of slice passed was > 250 (Discord role limit)")
	}

	roles := make([]string, 0, rSlice.Len())
	for i := 0; i < rSlice.Len(); i++ {
		switch v := rSlice.Index(i).Interface().(type) {
		case string:
			roles = append(roles, v)
		case int, int64:
			roles = append(roles, discordgo.StrID(reflect.ValueOf(v).Int()))
		default:
			return "", errors.New("Could not convert slice to string slice")
		}
	}

	err := common.BotSession.GuildMemberEdit(c.GS.ID, targetID, roles)
	if err != nil {
		return "", err
	}
	return "", nil
}

func (c *Context) tmplAddRoleID(role interface{}) (string, error) {
	if c.IncreaseCheckGenericAPICall() {
		return "", ErrTooManyAPICalls
	}

	if c.MS == nil {
		return "", nil
	}

	rid := ToInt64(role)
	if rid == 0 {
		return "", errors.New("no role id specified")
	}

	err := common.AddRoleDS(c.MS, rid)
	if err != nil {
		return "", err
	}

	return "", nil
}

func (c *Context) tmplAddRoleName(name string) (string, error) {
	if c.IncreaseCheckGenericAPICall() {
		return "", ErrTooManyAPICalls
	}

	if c.MS == nil {
		return "", nil
	}

	role := int64(0)
	for _, r := range c.GS.Roles {
		if strings.EqualFold(r.Name, name) {
			role = r.ID
			break
		}
	}

	if role == 0 {
		return "", errors.New("No Role with name " + name + " found")
	}

	if err := common.AddRoleDS(c.MS, role); err != nil {
		return "", err
	}

	return "", nil
}

func (c *Context) tmplRemoveRoleID(role interface{}, optionalArgs ...interface{}) (string, error) {
	if c.IncreaseCheckGenericAPICall() {
		return "", ErrTooManyAPICalls
	}

	delay := 0
	if len(optionalArgs) > 0 {
		delay = tmplToInt(optionalArgs[0])
	}

	rid := ToInt64(role)
	if rid == 0 {
		return "", errors.New("no role id specified")
	}

	if c.GS.GetRole(rid) == nil {
		return "", errors.New("unknown role")
	}

	if delay > 0 {
		scheduledevents2.ScheduleRemoveRole(context.Background(), c.GS.ID, c.MS.User.ID, rid, time.Now().Add(time.Second*time.Duration(delay)))
	} else {
		common.RemoveRoleDS(c.MS, rid)
	}

	return "", nil
}

func (c *Context) tmplRemoveRoleName(name string, optionalArgs ...interface{}) (string, error) {
	if c.IncreaseCheckGenericAPICall() {
		return "", ErrTooManyAPICalls
	}

	delay := 0
	if len(optionalArgs) > 0 {
		delay = tmplToInt(optionalArgs[0])
	}

	if c.MS == nil {
		return "", nil
	}

	role := c.findRoleByName(name)
	if role == nil {
		return "", errors.New("No Role with name " + name + " found")
	}

	if delay > 0 {
		scheduledevents2.ScheduleRemoveRole(context.Background(), c.GS.ID, c.MS.User.ID, role.ID, time.Now().Add(time.Second*time.Duration(delay)))
	} else {
		if err := common.RemoveRoleDS(c.MS, role.ID); err != nil {
			return "", err
		}
	}

	return "", nil
}

func (c *Context) findRoleByName(name string) *discordgo.Role {
	for _, r := range c.GS.Roles {
		if strings.EqualFold(r.Name, name) {
			return &r
		}
	}

	return nil
}

func (c *Context) tmplDelResponse(args ...interface{}) string {
	dur := 10
	if len(args) > 0 {
		dur = int(ToInt64(args[0]))
	}
	if dur > 86400 {
		dur = 86400
	}

	c.CurrentFrame.DelResponseDelay = dur
	c.CurrentFrame.DelResponse = true
	return ""
}

func (c *Context) tmplDelTrigger(args ...interface{}) string {
	if c.Msg != nil {
		return c.tmplDelMessage(c.Msg.ChannelID, c.Msg.ID, args...)
	}

	return ""
}

func (c *Context) tmplDelMessage(channel, msgID interface{}, args ...interface{}) string {
	cID := c.ChannelArgNoDM(channel)
	if cID == 0 {
		return ""
	}

	mID := ToInt64(msgID)

	dur := 10
	if len(args) > 0 {
		dur = int(ToInt64(args[0]))
	}

	if dur > 86400 {
		dur = 86400
	}

	MaybeScheduledDeleteMessage(c.GS.ID, cID, mID, dur)

	return ""
}

//Deletes reactions from a message either via reaction trigger or argument-set of emojis,
//needs channelID, messageID, userID, list of emojis - up to twenty
//can be run once per CC.
func (c *Context) tmplDelMessageReaction(values ...reflect.Value) (reflect.Value, error) {

	f := func(args []reflect.Value) (reflect.Value, error) {
		if len(args) < 4 {
			return reflect.Value{}, errors.New("not enough arguments (need channelID, messageID, userID, emoji)")
		}

		var cArg interface{}
		if args[0].IsValid() {
			cArg = args[0].Interface()
		}

		cID := c.ChannelArg(cArg)
		if cID == 0 {
			return reflect.ValueOf("non-existing channel"), nil
		}

		mID := ToInt64(args[1].Interface())
		uID := targetUserID(args[2].Interface())

		for _, reaction := range args[3:] {

			if c.IncreaseCheckCallCounter("del_reaction_message", 10) {
				return reflect.Value{}, ErrTooManyCalls
			}

			if err := common.BotSession.MessageReactionRemove(cID, mID, reaction.String(), uID); err != nil {
				return reflect.Value{}, err
			}
		}
		return reflect.ValueOf(""), nil
	}

	return callVariadic(f, false, values...)
}

func (c *Context) tmplDelAllMessageReactions(values ...reflect.Value) (reflect.Value, error) {

	f := func(args []reflect.Value) (reflect.Value, error) {
		if len(args) < 2 {
			return reflect.Value{}, errors.New("not enough arguments (need channelID, messageID, emojis[optional])")
		}

		var cArg interface{}
		if args[0].IsValid() {
			cArg = args[0].Interface()
		}

		cID := c.ChannelArg(cArg)
		if cID == 0 {
			return reflect.ValueOf("non-existing channel"), nil
		}

		mID := ToInt64(args[1].Interface())

		if len(args) > 2 {
			for _, emoji := range args[2:] {
				if c.IncreaseCheckCallCounter("del_reaction_message", 10) {
					return reflect.Value{}, ErrTooManyCalls
				}

				if err := common.BotSession.MessageReactionRemoveEmoji(cID, mID, emoji.String()); err != nil {
					return reflect.Value{}, err
				}
			}
			return reflect.ValueOf(""), nil
		}

		if c.IncreaseCheckGenericAPICall() {
			return reflect.Value{}, ErrTooManyAPICalls
		}
		common.BotSession.MessageReactionsRemoveAll(cID, mID)
		return reflect.ValueOf(""), nil
	}

	return callVariadic(f, false, values...)
}

func (c *Context) tmplGetMessage(channel, msgID interface{}) (*discordgo.Message, error) {
	if c.IncreaseCheckGenericAPICall() {
		return nil, ErrTooManyAPICalls
	}

	cID := c.ChannelArgNoDM(channel)
	if cID == 0 {
		return nil, nil
	}

	mID := ToInt64(msgID)

	message, _ := common.BotSession.ChannelMessage(cID, mID)
	return message, nil
}

func (c *Context) tmplGetMember(target interface{}) (*discordgo.Member, error) {
	if c.IncreaseCheckGenericAPICall() {
		return nil, ErrTooManyAPICalls
	}

	mID := targetUserID(target)
	if mID == 0 {
		return nil, nil
	}

	member, _ := bot.GetMember(c.GS.ID, mID)
	if member == nil {
		return nil, nil
	}

	return member.DgoMember(), nil
}

func (c *Context) tmplGetChannel(channel interface{}) (*CtxChannel, error) {

	if c.IncreaseCheckGenericAPICall() {
		return nil, ErrTooManyAPICalls
	}

	cID := c.ChannelArg(channel)
	if cID == 0 {
		return nil, nil //dont send an error , a nil output would indicate invalid/unknown channel
	}

	cstate := c.GS.GetChannel(cID)

	if cstate == nil {
		return nil, errors.New("channel not in state")
	}

	return CtxChannelFromCS(cstate), nil
}

func (c *Context) tmplAddReactions(values ...reflect.Value) (reflect.Value, error) {
	f := func(args []reflect.Value) (reflect.Value, error) {
		if c.Msg == nil {
			return reflect.Value{}, nil
		}

		for _, reaction := range args {
			if c.IncreaseCheckCallCounter("add_reaction_trigger", 20) {
				return reflect.Value{}, ErrTooManyCalls
			}

			if err := common.BotSession.MessageReactionAdd(c.Msg.ChannelID, c.Msg.ID, reaction.String()); err != nil {
				return reflect.Value{}, err
			}
		}
		return reflect.ValueOf(""), nil
	}

	return callVariadic(f, true, values...)
}

func (c *Context) tmplAddResponseReactions(values ...reflect.Value) (reflect.Value, error) {
	f := func(args []reflect.Value) (reflect.Value, error) {
		for _, reaction := range args {
			if c.IncreaseCheckCallCounter("add_reaction_response", 20) {
				return reflect.Value{}, ErrTooManyCalls
			}

			c.CurrentFrame.AddResponseReactionNames = append(c.CurrentFrame.AddResponseReactionNames, reaction.String())
		}
		return reflect.ValueOf(""), nil
	}

	return callVariadic(f, true, values...)
}

func (c *Context) tmplAddMessageReactions(values ...reflect.Value) (reflect.Value, error) {
	f := func(args []reflect.Value) (reflect.Value, error) {
		if len(args) < 2 {
			return reflect.Value{}, errors.New("not enough arguments (need channel and message-id)")
		}

		// cArg := args[0].Interface()
		var cArg interface{}
		if args[0].IsValid() {
			cArg = args[0].Interface()
		}

		cID := c.ChannelArg(cArg)
		mID := ToInt64(args[1].Interface())

		if cID == 0 {
			return reflect.ValueOf(""), nil
		}

		for i, reaction := range args {
			if i < 2 {
				continue
			}

			if c.IncreaseCheckCallCounter("add_reaction_message", 20) {
				return reflect.Value{}, ErrTooManyCalls
			}

			if err := common.BotSession.MessageReactionAdd(cID, mID, reaction.String()); err != nil {
				return reflect.Value{}, err
			}
		}
		return reflect.ValueOf(""), nil
	}

	return callVariadic(f, false, values...)
}

func (c *Context) tmplCurrentUserAgeHuman() string {
	t := bot.SnowflakeToTime(c.MS.User.ID)

	humanized := common.HumanizeDuration(common.DurationPrecisionHours, time.Since(t))
	if humanized == "" {
		humanized = "Less than an hour"
	}

	return humanized
}

func (c *Context) tmplCurrentUserAgeMinutes() int {
	t := bot.SnowflakeToTime(c.MS.User.ID)
	d := time.Since(t)

	return int(d.Seconds() / 60)
}

func (c *Context) tmplCurrentUserCreated() time.Time {
	t := bot.SnowflakeToTime(c.MS.User.ID)
	return t
}

func (c *Context) tmplSleep(duration interface{}) (string, error) {
	seconds := tmplToInt(duration)
	if c.secondsSlept+seconds > 60 || seconds < 1 {
		return "", errors.New("can sleep for max 60 seconds combined")
	}

	c.secondsSlept += seconds
	time.Sleep(time.Duration(seconds) * time.Second)
	return "", nil
}

func (c *Context) compileRegex(r string) (*regexp.Regexp, error) {
	if c.RegexCache == nil {
		c.RegexCache = make(map[string]*regexp.Regexp)
	}

	cached, ok := c.RegexCache[r]
	if ok {
		return cached, nil
	}

	if len(c.RegexCache) >= 10 {
		return nil, ErrTooManyAPICalls
	}

	compiled, err := regexp.Compile(r)
	if err != nil {
		return nil, err
	}

	c.RegexCache[r] = compiled

	return compiled, nil
}

func (c *Context) reFind(r, s string) (string, error) {
	compiled, err := c.compileRegex(r)
	if err != nil {
		return "", err
	}

	return compiled.FindString(s), nil
}

func (c *Context) reFindAll(r, s string, i ...int) ([]string, error) {
	compiled, err := c.compileRegex(r)
	if err != nil {
		return nil, err
	}

	var n int
	if len(i) > 0 {
		n = i[0]
	}

	if n > 1000 || n <= 0 {
		n = 1000
	}

	return compiled.FindAllString(s, n), nil
}

func (c *Context) reFindAllSubmatches(r, s string, i ...int) ([][]string, error) {
	compiled, err := c.compileRegex(r)
	if err != nil {
		return nil, err
	}

	var n int
	if len(i) > 0 {
		n = i[0]
	}

	if n > 100 || n <= 0 {
		n = 100
	}

	return compiled.FindAllStringSubmatch(s, n), nil
}

func (c *Context) reReplace(r, s, repl string) (string, error) {
	compiled, err := c.compileRegex(r)
	if err != nil {
		return "", err
	}

	return compiled.ReplaceAllString(s, repl), nil
}

func (c *Context) reSplit(r, s string, i ...int) ([]string, error) {
	compiled, err := c.compileRegex(r)
	if err != nil {
		return nil, err
	}

	var n int
	if len(i) > 0 {
		n = i[0]
	}

	if n > 500 || n <= 0 {
		n = 500
	}

	return compiled.Split(s, n), nil
}

func (c *Context) tmplEditChannelName(channel interface{}, newName string) (string, error) {
	if c.IncreaseCheckCallCounter("edit_channel", 10) {
		return "", ErrTooManyCalls
	}

	cID := c.ChannelArgNoDM(channel)
	if cID == 0 {
		return "", errors.New("unknown channel")
	}

	if c.IncreaseCheckCallCounter("edit_channel_"+strconv.FormatInt(cID, 10), 2) {
		return "", ErrTooManyCalls
	}

	_, err := common.BotSession.ChannelEdit(cID, newName)
	return "", err
}

func (c *Context) tmplEditChannelTopic(channel interface{}, newTopic string) (string, error) {
	if c.IncreaseCheckCallCounter("edit_channel", 10) {
		return "", ErrTooManyCalls
	}

	cID := c.ChannelArgNoDM(channel)
	if cID == 0 {
		return "", errors.New("unknown channel")
	}

	if c.IncreaseCheckCallCounter("edit_channel_"+strconv.FormatInt(cID, 10), 2) {
		return "", ErrTooManyCalls
	}

	edit := &discordgo.ChannelEdit{
		Topic: newTopic,
	}

	_, err := common.BotSession.ChannelEditComplex(cID, edit)
	return "", err
}

// DEPRECATED: this function will return unreliable numbers anyways
func (c *Context) tmplOnlineCount() (int, error) {
	// if c.IncreaseCheckCallCounter("online_users", 1) {
	// return 0, ErrTooManyCalls
	// }

	// online := 0
	// for _, v := range c.GS.Members {
	// 	if v.PresenceSet && v.PresenceStatus != dstate.StatusOffline {
	// 		online++
	// 	}
	// }

	return 0, nil
}

func (c *Context) tmplOnlineCountBots() (int, error) {
	// if c.IncreaseCheckCallCounter("online_bots", 1) {
	// 	return 0, ErrTooManyCalls
	// }

	// botCount := 0

	// for _, v := range c.GS.Members {
	// 	if v.Bot && v.PresenceSet && v.PresenceStatus != dstate.StatusOffline {
	// 		botCount++
	// 	}
	// }

	return 0, nil
}

func (c *Context) tmplEditNickname(Nickname string) (string, error) {

	if c.IncreaseCheckCallCounter("edit_nick", 2) {
		return "", ErrTooManyCalls
	}

	if c.MS == nil {
		return "", nil
	}

	if strings.Compare(c.MS.Member.Nick, Nickname) == 0 {

		return "", nil

	}

	err := common.BotSession.GuildMemberNickname(c.GS.ID, c.MS.User.ID, Nickname)
	if err != nil {
		return "", err
	}

	return "", nil
}
