// Package aichat OpenAI聊天
package aichat

import (
	"math/rand"
	"os"
	"strconv"
	"strings"

	"github.com/fumiama/deepinfra"
	"github.com/fumiama/deepinfra/model"
	"github.com/sirupsen/logrus"

	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"

	"github.com/FloatTech/floatbox/file"
	"github.com/FloatTech/floatbox/process"
	ctrl "github.com/FloatTech/zbpctrl"
	"github.com/FloatTech/zbputils/chat"
	"github.com/FloatTech/zbputils/control"
)

var (
	// en data [4 cfg] [4 type] [8 temp] [8 rate] LSB
	en = control.AutoRegister(&ctrl.Options[*zero.Ctx]{
		DisableOnDefault: false,
		Extra:            control.ExtraFromString("aichat"),
		Brief:            "OpenAI聊天",
		Help: "- 设置AI聊天触发概率10\n" +
			"- 设置AI聊天温度80\n" +
			"- 设置AI聊天接口类型[OpenAI|OLLaMA|GenAI]\n" +
			"- 设置AI聊天(不)支持系统提示词\n" +
			"- 设置AI聊天接口地址https://xxx\n" +
			"- 设置AI聊天密钥xxx\n" +
			"- 设置AI聊天模型名xxx\n" +
			"- 查看AI聊天系统提示词\n" +
			"- 重置AI聊天系统提示词\n" +
			"- 设置AI聊天系统提示词xxx\n" +
			"- 设置AI聊天分隔符</think>(留空则清除)\n" +
			"- 设置AI聊天(不)响应AT",
		PrivateDataFolder: "aichat",
	})
)

var (
	modelname      = model.ModelDeepDeek
	systemprompt   = chat.SystemPrompt
	api            = deepinfra.OpenAIDeepInfra
	sepstr         = ""
	noreplyat      = false
	nosystemprompt = false
)

var apitypes = map[string]uint8{
	"OpenAI": 0,
	"OLLaMA": 1,
	"GenAI":  2,
}

func init() {
	mf := en.DataFolder() + "model.txt"
	sf := en.DataFolder() + "system.txt"
	pf := en.DataFolder() + "sep.txt"
	af := en.DataFolder() + "api.txt"
	nf := en.DataFolder() + "NoReplyAT"
	syspf := en.DataFolder() + "NoSystemPrompt"
	if file.IsExist(mf) {
		data, err := os.ReadFile(mf)
		if err != nil {
			logrus.Warnln("read model", err)
		} else {
			modelname = string(data)
		}
	}
	if file.IsExist(sf) {
		data, err := os.ReadFile(sf)
		if err != nil {
			logrus.Warnln("read system", err)
		} else {
			systemprompt = string(data)
		}
	}
	if file.IsExist(pf) {
		data, err := os.ReadFile(pf)
		if err != nil {
			logrus.Warnln("read sep", err)
		} else {
			sepstr = string(data)
		}
	}
	if file.IsExist(af) {
		data, err := os.ReadFile(af)
		if err != nil {
			logrus.Warnln("read api", err)
		} else {
			api = string(data)
		}
	}
	noreplyat = file.IsExist(nf)
	nosystemprompt = file.IsExist(syspf)

	en.OnMessage(func(ctx *zero.Ctx) bool {
		return ctx.ExtractPlainText() != "" && (!noreplyat || (noreplyat && !ctx.Event.IsToMe))
	}).SetBlock(false).Handle(func(ctx *zero.Ctx) {
		gid := ctx.Event.GroupID
		if gid == 0 {
			gid = -ctx.Event.UserID
		}
		c, ok := ctx.State["manager"].(*ctrl.Control[*zero.Ctx])
		if !ok {
			return
		}
		rate := c.GetData(gid)
		temp := (rate >> 8) & 0xff
		typ := (rate >> 16) & 0x0f
		rate &= 0xff
		if !ctx.Event.IsToMe && rand.Intn(100) >= int(rate) {
			return
		}
		if ctx.Event.IsToMe {
			ctx.Block()
		}
		key := ""
		err := c.GetExtra(&key)
		if err != nil {
			logrus.Warnln("ERROR: get extra err:", err)
			return
		}
		if key == "" {
			logrus.Warnln("ERROR: get extra err: empty key")
			return
		}

		if temp <= 0 {
			temp = 70 // default setting
		}
		if temp > 100 {
			temp = 100
		}

		x := deepinfra.NewAPI(api, key)
		var mod model.Protocol

		switch typ {
		case 0:
			mod = model.NewOpenAI(
				modelname, sepstr,
				float32(temp)/100, 0.9, 4096,
			)
		case 1:
			mod = model.NewOLLaMA(
				modelname, sepstr,
				float32(temp)/100, 0.9, 4096,
			)
		case 2:
			mod = model.NewGenAI(
				modelname,
				float32(temp)/100, 0.9, 4096,
			)
		default:
			logrus.Warnln("[aichat] unsupported AI type", typ)
			return
		}

		data, err := x.Request(chat.Ask(mod, gid, systemprompt, nosystemprompt))
		if err != nil {
			logrus.Warnln("[aichat] post err:", err)
			return
		}

		txt := chat.Sanitize(strings.Trim(data, "\n 　"))
		if len(txt) > 0 {
			chat.Reply(gid, txt)
			nick := zero.BotConfig.NickName[rand.Intn(len(zero.BotConfig.NickName))]
			txt = strings.ReplaceAll(txt, "{name}", ctx.CardOrNickName(ctx.Event.UserID))
			txt = strings.ReplaceAll(txt, "{me}", nick)
			id := any(nil)
			if ctx.Event.IsToMe {
				id = ctx.Event.MessageID
			}
			for _, t := range strings.Split(txt, "{segment}") {
				if t == "" {
					continue
				}
				if id != nil {
					id = ctx.SendChain(message.Reply(id), message.Text(t))
				} else {
					id = ctx.SendChain(message.Text(t))
				}
				process.SleepAbout1sTo2s()
			}
		}
	})
	en.OnPrefix("设置AI聊天触发概率", zero.AdminPermission).SetBlock(true).Handle(func(ctx *zero.Ctx) {
		args := strings.TrimSpace(ctx.State["args"].(string))
		if args == "" {
			ctx.SendChain(message.Text("ERROR: empty args"))
			return
		}
		c, ok := ctx.State["manager"].(*ctrl.Control[*zero.Ctx])
		if !ok {
			ctx.SendChain(message.Text("ERROR: no such plugin"))
			return
		}
		r, err := strconv.Atoi(args)
		if err != nil {
			ctx.SendChain(message.Text("ERROR: parse rate err: ", err))
			return
		}
		if r > 100 {
			r = 100
		} else if r < 0 {
			r = 0
		}
		gid := ctx.Event.GroupID
		if gid == 0 {
			gid = -ctx.Event.UserID
		}
		val := c.GetData(gid) & (^0xff)
		err = c.SetData(gid, val|int64(r&0xff))
		if err != nil {
			ctx.SendChain(message.Text("ERROR: set data err: ", err))
			return
		}
		ctx.SendChain(message.Text("成功"))
	})
	en.OnPrefix("设置AI聊天温度", zero.AdminPermission).SetBlock(true).Handle(func(ctx *zero.Ctx) {
		args := strings.TrimSpace(ctx.State["args"].(string))
		if args == "" {
			ctx.SendChain(message.Text("ERROR: empty args"))
			return
		}
		c, ok := ctx.State["manager"].(*ctrl.Control[*zero.Ctx])
		if !ok {
			ctx.SendChain(message.Text("ERROR: no such plugin"))
			return
		}
		r, err := strconv.Atoi(args)
		if err != nil {
			ctx.SendChain(message.Text("ERROR: parse rate err: ", err))
			return
		}
		if r > 100 {
			r = 100
		} else if r < 0 {
			r = 0
		}
		gid := ctx.Event.GroupID
		if gid == 0 {
			gid = -ctx.Event.UserID
		}
		val := c.GetData(gid) & (^0xff00)
		err = c.SetData(gid, val|(int64(r&0xff)<<8))
		if err != nil {
			ctx.SendChain(message.Text("ERROR: set data err: ", err))
			return
		}
		ctx.SendChain(message.Text("成功"))
	})
	en.OnPrefix("设置AI聊天接口类型", zero.SuperUserPermission).SetBlock(true).Handle(func(ctx *zero.Ctx) {
		args := strings.TrimSpace(ctx.State["args"].(string))
		if args == "" {
			ctx.SendChain(message.Text("ERROR: empty args"))
			return
		}
		c, ok := ctx.State["manager"].(*ctrl.Control[*zero.Ctx])
		if !ok {
			ctx.SendChain(message.Text("ERROR: no such plugin"))
			return
		}
		typ, ok := apitypes[args]
		if !ok {
			ctx.SendChain(message.Text("ERROR: 未知类型 ", args))
			return
		}
		gid := ctx.Event.GroupID
		if gid == 0 {
			gid = -ctx.Event.UserID
		}
		val := c.GetData(gid) & (^0x0f0000)
		err := c.SetData(gid, val|(int64(typ&0x0f)<<16))
		if err != nil {
			ctx.SendChain(message.Text("ERROR: set data err: ", err))
			return
		}
		ctx.SendChain(message.Text("成功"))
	})
	en.OnPrefix("设置AI聊天接口地址", zero.OnlyPrivate, zero.SuperUserPermission).SetBlock(true).Handle(func(ctx *zero.Ctx) {
		args := strings.TrimSpace(ctx.State["args"].(string))
		if args == "" {
			ctx.SendChain(message.Text("ERROR: empty args"))
			return
		}
		api = args
		err := os.WriteFile(af, []byte(args), 0644)
		if err != nil {
			ctx.SendChain(message.Text("ERROR: ", err))
			return
		}
		ctx.SendChain(message.Text("成功"))
	})
	en.OnPrefix("设置AI聊天密钥", zero.OnlyPrivate, zero.SuperUserPermission).SetBlock(true).Handle(func(ctx *zero.Ctx) {
		args := strings.TrimSpace(ctx.State["args"].(string))
		if args == "" {
			ctx.SendChain(message.Text("ERROR: empty args"))
			return
		}
		c, ok := ctx.State["manager"].(*ctrl.Control[*zero.Ctx])
		if !ok {
			ctx.SendChain(message.Text("ERROR: no such plugin"))
			return
		}
		err := c.SetExtra(&args)
		if err != nil {
			ctx.SendChain(message.Text("ERROR: ", err))
			return
		}
		ctx.SendChain(message.Text("成功"))
	})
	en.OnPrefix("设置AI聊天模型名", zero.OnlyPrivate, zero.SuperUserPermission).SetBlock(true).Handle(func(ctx *zero.Ctx) {
		args := strings.TrimSpace(ctx.State["args"].(string))
		if args == "" {
			ctx.SendChain(message.Text("ERROR: empty args"))
			return
		}
		modelname = args
		err := os.WriteFile(mf, []byte(args), 0644)
		if err != nil {
			ctx.SendChain(message.Text("ERROR: ", err))
			return
		}
		ctx.SendChain(message.Text("成功"))
	})
	en.OnPrefix("设置AI聊天系统提示词", zero.OnlyPrivate, zero.SuperUserPermission).SetBlock(true).Handle(func(ctx *zero.Ctx) {
		args := strings.TrimSpace(ctx.State["args"].(string))
		if args == "" {
			ctx.SendChain(message.Text("ERROR: empty args"))
			return
		}
		systemprompt = args
		err := os.WriteFile(sf, []byte(args), 0644)
		if err != nil {
			ctx.SendChain(message.Text("ERROR: ", err))
			return
		}
		ctx.SendChain(message.Text("成功"))
	})
	en.OnFullMatch("查看AI聊天系统提示词", zero.OnlyPrivate, zero.SuperUserPermission).SetBlock(true).Handle(func(ctx *zero.Ctx) {
		ctx.SendChain(message.Text(systemprompt))
	})
	en.OnFullMatch("重置AI聊天系统提示词", zero.OnlyPrivate, zero.SuperUserPermission).SetBlock(true).Handle(func(ctx *zero.Ctx) {
		systemprompt = chat.SystemPrompt
		_ = os.Remove(sf)
		ctx.SendChain(message.Text("成功"))
	})
	en.OnPrefix("设置AI聊天分隔符", zero.OnlyPrivate, zero.SuperUserPermission).SetBlock(true).Handle(func(ctx *zero.Ctx) {
		args := strings.TrimSpace(ctx.State["args"].(string))
		if args == "" {
			sepstr = ""
			_ = os.Remove(pf)
			ctx.SendChain(message.Text("清除成功"))
			return
		}
		sepstr = args
		err := os.WriteFile(pf, []byte(args), 0644)
		if err != nil {
			ctx.SendChain(message.Text("ERROR: ", err))
			return
		}
		ctx.SendChain(message.Text("设置成功"))
	})
	en.OnRegex("^设置AI聊天(不)?响应AT$", zero.OnlyPrivate, zero.SuperUserPermission).SetBlock(true).Handle(func(ctx *zero.Ctx) {
		args := ctx.State["regex_matched"].([]string)
		isno := args[1] == "不"
		if isno {
			f, err := os.Create(nf)
			if err != nil {
				ctx.SendChain(message.Text("ERROR: ", err))
				return
			}
			defer f.Close()
			_, err = f.WriteString("PLACEHOLDER")
			if err != nil {
				ctx.SendChain(message.Text("ERROR: ", err))
				return
			}
			noreplyat = true
		} else {
			_ = os.Remove(nf)
			noreplyat = false
		}
		ctx.SendChain(message.Text("成功"))
	})
	en.OnRegex("^设置AI聊天(不)?支持系统提示词$", zero.OnlyPrivate, zero.SuperUserPermission).SetBlock(true).Handle(func(ctx *zero.Ctx) {
		args := ctx.State["regex_matched"].([]string)
		isno := args[1] == "不"
		if isno {
			f, err := os.Create(syspf)
			if err != nil {
				ctx.SendChain(message.Text("ERROR: ", err))
				return
			}
			defer f.Close()
			_, err = f.WriteString("PLACEHOLDER")
			if err != nil {
				ctx.SendChain(message.Text("ERROR: ", err))
				return
			}
			nosystemprompt = true
		} else {
			_ = os.Remove(syspf)
			nosystemprompt = false
		}
		ctx.SendChain(message.Text("成功"))
	})
}
