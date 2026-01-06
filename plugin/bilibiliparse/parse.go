// Package bilibiliparse bilibili卡片解析
package bilibiliparse

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	bz "github.com/FloatTech/AnimeAPI/bilibili"
	"github.com/FloatTech/floatbox/file"
	"github.com/FloatTech/floatbox/web"
	ctrl "github.com/FloatTech/zbpctrl"
	"github.com/FloatTech/zbputils/control"
	"github.com/FloatTech/zbputils/ctxext"
	"github.com/pkg/errors"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
)

const (
	enableVideoSummary   = int64(0x10)
	disableVideoSummary  = ^enableVideoSummary
	enableVideoDownload  = int64(0x20)
	disableVideoDownload = ^enableVideoDownload
	enableVideoInfo      = int64(0x40)
	disableVideoInfo     = ^enableVideoInfo
	// 视频限制类型标志
	useTimeLimit         = int64(0x80)  // 使用时长限制
	useSizeLimit         = int64(0x100) // 使用大小限制
	bilibiliparseReferer = "https://www.bilibili.com"
	ua                   = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/92.0.4515.107 Safari/537.36"
)

var (
	limit            = ctxext.NewLimiterManager(time.Second*10, 1)
	searchVideo      = `bilibili.com\\?/video\\?/(?:av(\d+)|([bB][vV][0-9a-zA-Z]+))`
	searchDynamic    = `(t.bilibili.com|m.bilibili.com\\?/dynamic)\\?/(\d+)`
	searchArticle    = `bilibili.com\\?/read\\?/(?:cv|mobile\\?/)(\d+)`
	searchLiveRoom   = `live.bilibili.com\\?/(\d+)`
	searchVideoRe    = regexp.MustCompile(searchVideo)
	searchDynamicRe  = regexp.MustCompile(searchDynamic)
	searchArticleRe  = regexp.MustCompile(searchArticle)
	searchLiveRoomRe = regexp.MustCompile(searchLiveRoom)
	errFFmpegMissing = errors.New("未配置ffmpeg")
	cachePath        string
	dataFolder       string // 存储数据文件夹路径
	cfg              = bz.NewCookieConfig("data/Bilibili/config.json")
	// 默认视频时长限制（秒）
	defaultVideoTimeLimit = 480 // 8分钟
	// 默认视频大小限制（MB）
	defaultVideoSizeLimit = 100 // 100MB
)

// 插件主体
func init() {
	en := control.AutoRegister(&ctrl.Options[*zero.Ctx]{
		DisableOnDefault: false,
		Brief:            "b站链接解析",
		Help:             "例:- t.bilibili.com/642277677329285174\n- bilibili.com/read/cv17134450\n- bilibili.com/video/BV13B4y1x7pS\n- live.bilibili.com/22603245 ",
	})
	cachePath = en.DataFolder() + "cache/"
	dataFolder = en.DataFolder()
	_ = os.RemoveAll(cachePath)
	_ = os.MkdirAll(cachePath, 0755)
	en.OnRegex(`((b23|acg).tv|bili2233.cn)\\?/[0-9a-zA-Z]+`).SetBlock(true).Limit(limit.LimitByGroup).
		Handle(func(ctx *zero.Ctx) {
			u := ctx.State["regex_matched"].([]string)[0]
			u = strings.ReplaceAll(u, "\\", "")
			realurl, err := bz.GetRealURL("https://" + u)
			if err != nil {
				ctx.SendChain(message.Text("ERROR: ", err))
				return
			}
			switch {
			case searchVideoRe.MatchString(realurl):
				ctx.State["regex_matched"] = searchVideoRe.FindStringSubmatch(realurl)
				handleVideo(ctx)
			case searchDynamicRe.MatchString(realurl):
				ctx.State["regex_matched"] = searchDynamicRe.FindStringSubmatch(realurl)
				handleDynamic(ctx)
			case searchArticleRe.MatchString(realurl):
				ctx.State["regex_matched"] = searchArticleRe.FindStringSubmatch(realurl)
				handleArticle(ctx)
			case searchLiveRoomRe.MatchString(realurl):
				ctx.State["regex_matched"] = searchLiveRoomRe.FindStringSubmatch(realurl)
				handleLive(ctx)
			}
		})
	en.OnRegex(`^(开启|打开|启用|关闭|关掉|禁用)视频总结$`, zero.AdminPermission).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			gid := ctx.Event.GroupID
			if gid <= 0 {
				// 个人用户设为负数
				gid = -ctx.Event.UserID
			}
			option := ctx.State["regex_matched"].([]string)[1]
			c, ok := ctx.State["manager"].(*ctrl.Control[*zero.Ctx])
			if !ok {
				ctx.SendChain(message.Text("找不到服务!"))
				return
			}
			data := c.GetData(ctx.Event.GroupID)
			switch option {
			case "开启", "打开", "启用":
				data |= enableVideoSummary
			case "关闭", "关掉", "禁用":
				data &= disableVideoSummary
			default:
				return
			}
			err := c.SetData(gid, data)
			if err != nil {
				ctx.SendChain(message.Text("出错啦: ", err))
				return
			}
			ctx.SendChain(message.Text("已", option, "视频总结"))
		})
	en.OnRegex(`^(开启|打开|启用|关闭|关掉|禁用)视频上传$`, zero.AdminPermission).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			gid := ctx.Event.GroupID
			if gid <= 0 {
				// 个人用户设为负数
				gid = -ctx.Event.UserID
			}
			option := ctx.State["regex_matched"].([]string)[1]
			c, ok := ctx.State["manager"].(*ctrl.Control[*zero.Ctx])
			if !ok {
				ctx.SendChain(message.Text("找不到服务!"))
				return
			}
			data := c.GetData(ctx.Event.GroupID)
			switch option {
			case "开启", "打开", "启用":
				data |= enableVideoDownload
			case "关闭", "关掉", "禁用":
				data &= disableVideoDownload
			default:
				return
			}
			err := c.SetData(gid, data)
			if err != nil {
				ctx.SendChain(message.Text("出错啦: ", err))
				return
			}
			ctx.SendChain(message.Text("已", option, "视频上传"))
		})
	en.OnRegex(`^(开启|打开|启用|关闭|关掉|禁用)视频信息$`, zero.AdminPermission).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			gid := ctx.Event.GroupID
			if gid <= 0 {
				// 个人用户设为负数
				gid = -ctx.Event.UserID
			}
			option := ctx.State["regex_matched"].([]string)[1]
			c, ok := ctx.State["manager"].(*ctrl.Control[*zero.Ctx])
			if !ok {
				ctx.SendChain(message.Text("找不到服务!"))
				return
			}
			data := c.GetData(ctx.Event.GroupID)
			switch option {
			case "开启", "打开", "启用":
				data |= enableVideoInfo
			case "关闭", "关掉", "禁用":
				data &= disableVideoInfo
			default:
				return
			}
			err := c.SetData(gid, data)
			if err != nil {
				ctx.SendChain(message.Text("出错啦: ", err))
				return
			}
			ctx.SendChain(message.Text("已", option, "视频信息"))
		})
	// 设置视频时长限制
	en.OnRegex(`^设置视频时长限制(\d+)(秒|分钟|小时)$`, zero.AdminPermission).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			gid := ctx.Event.GroupID
			if gid <= 0 {
				// 个人用户设为负数
				gid = -ctx.Event.UserID
			}
			durationStr := ctx.State["regex_matched"].([]string)[1]
			unit := ctx.State["regex_matched"].([]string)[2]
			c, ok := ctx.State["manager"].(*ctrl.Control[*zero.Ctx])
			if !ok {
				ctx.SendChain(message.Text("找不到服务!"))
				return
			}
			data := c.GetData(ctx.Event.GroupID)
			// 清除大小限制标志，设置时长限制标志
			data &= ^useSizeLimit
			data |= useTimeLimit

			// 计算时长（转换为秒）
			duration := 0
			fmt.Sscanf(durationStr, "%d", &duration)
			switch unit {
			case "秒":
				// 已经是秒，无需转换
			case "分钟":
				duration *= 60
			case "小时":
				duration *= 3600
			}

			// 保存时长限制到配置文件
			limitConfigPath := dataFolder + "video_limit.json"
			limitConfig := map[string]interface{}{
				"type":  "time",
				"value": duration,
			}
			configData, err := json.Marshal(limitConfig)
			if err != nil {
				ctx.SendChain(message.Text("配置序列化失败: ", err))
				return
			}
			err = os.WriteFile(limitConfigPath, configData, 0644)
			if err != nil {
				ctx.SendChain(message.Text("保存配置失败: ", err))
				return
			}

			err = c.SetData(gid, data)
			if err != nil {
				ctx.SendChain(message.Text("出错啦: ", err))
				return
			}

			// 格式化显示时长
			displayDuration := ""
			hours := duration / 3600
			minutes := (duration % 3600) / 60
			seconds := duration % 60
			if hours > 0 {
				displayDuration = fmt.Sprintf("%d小时%d分钟%d秒", hours, minutes, seconds)
			} else if minutes > 0 {
				displayDuration = fmt.Sprintf("%d分钟%d秒", minutes, seconds)
			} else {
				displayDuration = fmt.Sprintf("%d秒", seconds)
			}

			ctx.SendChain(message.Text("已设置视频时长限制为: ", displayDuration))
		})
	// 设置视频大小限制
	en.OnRegex(`^设置视频大小限制(\d+)(MB|GB)$`, zero.AdminPermission).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			gid := ctx.Event.GroupID
			if gid <= 0 {
				// 个人用户设为负数
				gid = -ctx.Event.UserID
			}
			sizeStr := ctx.State["regex_matched"].([]string)[1]
			unit := ctx.State["regex_matched"].([]string)[2]
			c, ok := ctx.State["manager"].(*ctrl.Control[*zero.Ctx])
			if !ok {
				ctx.SendChain(message.Text("找不到服务!"))
				return
			}
			data := c.GetData(ctx.Event.GroupID)
			// 清除时长限制标志，设置大小限制标志
			data &= ^useTimeLimit
			data |= useSizeLimit

			// 计算大小（转换为MB）
			size := 0
			fmt.Sscanf(sizeStr, "%d", &size)
			switch unit {
			case "MB":
				// 已经是MB，无需转换
			case "GB":
				size *= 1024
			}

			// 保存大小限制到配置文件
			limitConfigPath := dataFolder + "video_limit.json"
			limitConfig := map[string]interface{}{
				"type":  "size",
				"value": size,
			}
			configData, err := json.Marshal(limitConfig)
			if err != nil {
				ctx.SendChain(message.Text("配置序列化失败: ", err))
				return
			}
			err = os.WriteFile(limitConfigPath, configData, 0644)
			if err != nil {
				ctx.SendChain(message.Text("保存配置失败: ", err))
				return
			}

			err = c.SetData(gid, data)
			if err != nil {
				ctx.SendChain(message.Text("出错啦: ", err))
				return
			}

			// 格式化显示大小
			displaySize := ""
			if size >= 1024 {
				displaySize = fmt.Sprintf("%.1fGB", float64(size)/1024)
			} else {
				displaySize = fmt.Sprintf("%dMB", size)
			}

			ctx.SendChain(message.Text("已设置视频大小限制为: ", displaySize))
		})
	en.OnRegex(searchVideo).SetBlock(true).Limit(limit.LimitByGroup).Handle(handleVideo)
	en.OnRegex(searchDynamic).SetBlock(true).Limit(limit.LimitByGroup).Handle(handleDynamic)
	en.OnRegex(searchArticle).SetBlock(true).Limit(limit.LimitByGroup).Handle(handleArticle)
	en.OnRegex(searchLiveRoom).SetBlock(true).Limit(limit.LimitByGroup).Handle(handleLive)
}

func handleVideo(ctx *zero.Ctx) {
	id := ctx.State["regex_matched"].([]string)[1]
	if id == "" {
		id = ctx.State["regex_matched"].([]string)[2]
	}
	card, err := bz.GetVideoInfo(id)
	if err != nil {
		ctx.SendChain(message.Text("ERROR: ", err))
		return
	}
	c, ok := ctx.State["manager"].(*ctrl.Control[*zero.Ctx])
	if ok && c.GetData(ctx.Event.GroupID)&enableVideoInfo == enableVideoInfo {
		msg, err := card.ToVideoMessage()
		if err != nil {
			ctx.SendChain(message.Text("ERROR: ", err))
			return
		}
		if c.GetData(ctx.Event.GroupID)&enableVideoSummary == enableVideoSummary {
			summaryMsg, err := getVideoSummary(cfg, card)
			if err != nil {
				msg = append(msg, message.Text("ERROR: ", err))
			} else {
				msg = append(msg, summaryMsg...)
			}
		}
		ctx.SendChain(msg...)
	}
	if ok && c.GetData(ctx.Event.GroupID)&enableVideoDownload == enableVideoDownload {
		downLoadMsg, err := getVideoDownload(ctx, cfg, card, cachePath)
		if err != nil {
			if errors.Is(err, errFFmpegMissing) {
				return
			}
			ctx.SendChain(message.Text("ERROR: ", err))
			return
		}
		ctx.SendChain(downLoadMsg...)
	}
}

func handleDynamic(ctx *zero.Ctx) {
	msg, err := cfg.GetDetailMessage(ctx.State["regex_matched"].([]string)[2])
	if err != nil {
		ctx.SendChain(message.Text("ERROR: ", err))
		return
	}
	ctx.SendChain(msg...)
}

func handleArticle(ctx *zero.Ctx) {
	card, err := bz.GetArticleInfo(ctx.State["regex_matched"].([]string)[1])
	if err != nil {
		ctx.SendChain(message.Text("ERROR: ", err))
		return
	}
	ctx.SendChain(card.ToArticleMessage(ctx.State["regex_matched"].([]string)[1])...)
}

func handleLive(ctx *zero.Ctx) {
	cookie, err := cfg.Load()
	if err != nil {
		ctx.SendChain(message.Text("ERROR: ", err))
		return
	}
	card, err := bz.GetLiveRoomInfo(ctx.State["regex_matched"].([]string)[1], cookie)
	if err != nil {
		ctx.SendChain(message.Text("ERROR: ", err))
		return
	}
	ctx.SendChain(card.ToMessage()...)
}

// getVideoSummary AI视频总结
func getVideoSummary(cookiecfg *bz.CookieConfig, card bz.Card) (msg []message.Segment, err error) {
	var (
		data         []byte
		videoSummary bz.VideoSummary
	)
	data, err = web.RequestDataWithHeaders(web.NewDefaultClient(), bz.SignURL(fmt.Sprintf(bz.VideoSummaryURL, card.BvID, card.CID, card.Owner.Mid)), "GET", func(req *http.Request) error {
		if cookiecfg != nil {
			cookie := ""
			cookie, err = cookiecfg.Load()
			if err != nil {
				return err
			}
			req.Header.Add("cookie", cookie)
		}
		req.Header.Set("User-Agent", ua)
		return nil
	}, nil)
	if err != nil {
		return
	}
	err = json.Unmarshal(data, &videoSummary)
	msg = make([]message.Segment, 0, 16)
	msg = append(msg, message.Text("已为你生成视频总结\n\n"))
	msg = append(msg, message.Text(videoSummary.Data.ModelResult.Summary, "\n\n"))
	for _, v := range videoSummary.Data.ModelResult.Outline {
		msg = append(msg, message.Text("● ", v.Title, "\n"))
		for _, p := range v.PartOutline {
			msg = append(msg, message.Text(fmt.Sprintf("%d:%d %s\n", p.Timestamp/60, p.Timestamp%60, p.Content)))
		}
		msg = append(msg, message.Text("\n"))
	}
	return
}

func getVideoDownload(ctx *zero.Ctx, cookiecfg *bz.CookieConfig, card bz.Card, cachePath string) (msg []message.Segment, err error) {
	var (
		data          []byte
		videoDownload bz.VideoDownload
		stderr        bytes.Buffer
	)
	today := time.Now().Format("20060102")
	videoFile := fmt.Sprintf("%s%s%s.mp4", cachePath, card.BvID, today)
	if file.IsExist(videoFile) {
		msg = append(msg, message.Video("file:///"+file.BOTPATH+"/"+videoFile))
		return
	}
	data, err = web.RequestDataWithHeaders(web.NewDefaultClient(), bz.SignURL(fmt.Sprintf(bz.VideoDownloadURL, card.BvID, card.CID)), "GET", func(req *http.Request) error {
		if cookiecfg != nil {
			cookie := ""
			cookie, err = cookiecfg.Load()
			if err != nil {
				return err
			}
			req.Header.Add("cookie", cookie)
		}
		req.Header.Set("User-Agent", ua)
		return nil
	}, nil)
	if err != nil {
		return
	}
	err = json.Unmarshal(data, &videoDownload)
	if err != nil {
		return
	}
	headers := fmt.Sprintf("User-Agent: %s\nReferer: %s", ua, bilibiliparseReferer)
	// 限制最多下载8分钟视频
	c, ok := ctx.State["manager"].(*ctrl.Control[*zero.Ctx])
	var limitType string = "time" // 默认使用时长限制
	var limitValue int = defaultVideoTimeLimit

	if ok {
		gid := ctx.Event.GroupID
		if gid <= 0 {
			// 个人用户设为负数
			gid = -ctx.Event.UserID
		}
		data := c.GetData(gid)

		limitConfigPath := dataFolder + "video_limit.json"
		if file.IsExist(limitConfigPath) {
			configData, err := os.ReadFile(limitConfigPath)
			if err == nil {
				var limitConfig map[string]interface{}
				err = json.Unmarshal(configData, &limitConfig)
				if err == nil {
					if t, ok := limitConfig["type"].(string); ok {
						limitType = t
					}
					if v, ok := limitConfig["value"].(float64); ok {
						limitValue = int(v)
					}
				}
			}
		}

		if data&useTimeLimit == useTimeLimit {
			limitType = "time"
		} else if data&useSizeLimit == useSizeLimit {
			limitType = "size"
		}
	}

	var cmd *exec.Cmd
	if limitType == "time" {
		// 使用时长限制
		cmd = exec.Command("ffmpeg", "-ss", "0", "-t", fmt.Sprintf("%d", limitValue), "-headers", headers, "-i", videoDownload.Data.Durl[0].URL, "-c", "copy", videoFile)
	} else {
		// 使用大小限制，使用ffmpeg的-fs参数限制输出文件大小
		// 将MB转换为字节
		sizeLimitBytes := limitValue * 1024 * 1024
		cmd = exec.Command("ffmpeg", "-ss", "0", "-headers", headers, "-i", videoDownload.Data.Durl[0].URL, "-c", "copy", "-fs", fmt.Sprintf("%d", sizeLimitBytes), videoFile)
	}
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		fmt.Printf("[bilibiliparse] ffmpeg执行失败: %v\n", stderr.String())
		err = errors.Wrap(errFFmpegMissing, stderr.String())
		return
	}
	msg = append(msg, message.Video("file:///"+file.BOTPATH+"/"+videoFile))
	return
}
