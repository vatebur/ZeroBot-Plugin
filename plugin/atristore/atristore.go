// Package atristore ATRI商店
package atristore

import (
	"github.com/FloatTech/AnimeAPI/wallet"
	fcext "github.com/FloatTech/floatbox/ctxext"
	"github.com/FloatTech/floatbox/file"
	"github.com/FloatTech/floatbox/math"
	"github.com/FloatTech/gg"
	"github.com/FloatTech/imgfactory"
	sql "github.com/FloatTech/sqlite"
	ctrl "github.com/FloatTech/zbpctrl"
	"github.com/FloatTech/zbputils/control"
	"github.com/FloatTech/zbputils/ctxext"
	"github.com/FloatTech/zbputils/img/text"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/extension/single"
	"github.com/wdvxdr1123/ZeroBot/message"
	"image"
	"image/color"
	"strconv"
	"sync"
	"time"
)

type storeRepo struct {
	db *sql.Sqlite
	sync.RWMutex
}
type storeRecord struct {
	Name   string
	Number int
	Price  int
}
type userPackRecord struct {
	Duration int64
	Name     string
	Number   int
}

func init() {
	dbData := &storeRepo{
		db: &sql.Sqlite{},
	}

	engine := control.AutoRegister(&ctrl.Options[*zero.Ctx]{
		DisableOnDefault: false,
		Brief:            "ATRI币商店",
		Help: "- 实物商店\n" +
			"- 兑换[商品名称]\n",
		PrivateDataFolder: "atristore",
	}).ApplySingle(single.New(
		single.WithKeyFn(func(ctx *zero.Ctx) int64 { return ctx.Event.GroupID }),
		single.WithPostFn[int64](func(ctx *zero.Ctx) {
			ctx.Send(
				message.ReplyWithMessage(ctx.Event.MessageID,
					message.Text("别着急，警察局门口排长队了！"),
				),
			)
		}),
	))

	getdb := fcext.DoOnceOnSuccess(func(ctx *zero.Ctx) bool {
		dbData.db.DBPath = engine.DataFolder() + "atristore.db"
		err := dbData.db.Open(time.Hour)
		if err == nil {
			// 创建CD表
			err = dbData.db.Create("criminal_record", &storeRepo{})
			if err != nil {
				ctx.SendChain(message.Text("[ERROR]:", err))
				return false
			}
			return true
		}
		ctx.SendChain(message.Text("[ERROR]:", err))
		return false
	})

	engine.OnFullMatchGroup([]string{"实物商店"}, getdb).SetBlock(true).Limit(ctxext.LimitByUser).Handle(func(ctx *zero.Ctx) {
		infos, err := dbData.getStoreInfo()
		if err != nil {
			ctx.SendChain(message.Text("[ERROR at storeRecord]:", err))
			return
		}
		var picImage image.Image
		if len(infos) == 0 {
			picImage, err = drawStoreEmptyImage()
		} else {
			picImage, err = drawStoreInfoImage(infos)
		}
		if err != nil {
			ctx.SendChain(message.Text("[ERROR at storeRecord]:", err))
			return
		}
		pic, err := imgfactory.ToBytes(picImage)
		if err != nil {
			ctx.SendChain(message.Text("[ERROR at storeRecord]:", err))
			return
		}
		ctx.SendChain(message.ImageBytes(pic))
	})

	engine.OnRegex(`^兑换(.*)$`, getdb).SetBlock(true).Limit(ctxext.LimitByUser).Handle(func(ctx *zero.Ctx) {
		uid := ctx.Event.UserID
		thingName := ctx.State["regex_matched"].([]string)[1]

		thingInfos, err := dbData.getStoreThingInfo(thingName)
		if err != nil {
			ctx.SendChain(message.Text("[ERROR at store.go.11]:", err))
			return
		}
		if len(thingInfos) == 0 {
			ctx.SendChain(message.Text("当前商店并没有上架该物品"))
			return
		}
		ok, err := dbData.checkStoreFor(thingInfos[0])
		if err != nil {
			ctx.SendChain(message.Text("[ERROR at store.go.11]:", err))
			return
		}
		if ok {
			ctx.SendChain(message.Reply(ctx.Event.MessageID), message.Text("你慢了一步,物品被别人买走了"))
			return
		}
		money := wallet.GetWalletOf(uid)
		if money < thingInfos[0].Price*10000 {
			ctx.SendChain(message.Text("你身上的钱(", money, ")不够支付"))
			return
		}

		ctx.Send(message.ReplyWithMessage(ctx.Event.MessageID, message.Text("你确定", "购买", thingName, "?", "\n回答\"是\"或\"否\"")))
		// 等待用户下一步选择
		recv, cancel1 := zero.NewFutureEvent("message", 999, false, zero.RegexRule(`^(是|否)$`), zero.CheckUser(ctx.Event.UserID)).Repeat()
		defer cancel1()
		buy := false
		for {
			select {
			case <-time.After(time.Second * 60):
				ctx.Send(message.ReplyWithMessage(ctx.Event.MessageID, message.Text("等待超时,取消购买")))
				return
			case e := <-recv:
				nextcmd := e.Event.Message.String()
				if nextcmd == "否" {
					ctx.Send(message.ReplyWithMessage(ctx.Event.MessageID, message.Text("已取消购买")))
					return
				}
				buy = true
			}
			if buy {
				break
			}
		}

		if thingInfos[0].Number <= 0 {
			ctx.Send(message.ReplyWithMessage(ctx.Event.MessageID, message.Text("商店数量不足")))
			return
		}
		thingInfos[0].Number -= 1
		err = dbData.updateStoreInfo(thingInfos[0])
		if err != nil {
			ctx.SendChain(message.Text("[ERROR at store.go.12]:", err))
			return
		}
		err = wallet.InsertWalletOf(uid, -thingInfos[0].Price*10000)
		if err != nil {
			ctx.SendChain(message.Text("[ERROR at store.go.13]:", err))
			return
		}

		userid := ctx.Event.UserID
		username := ctx.CardOrNickName(userid)
		for _, su := range zero.BotConfig.SuperUsers {
			msg := username + "(QQ:" + strconv.FormatInt(userid, 10) + "),购买了" + thingName + "请安排发货"
			ctx.SendPrivateMessage(su, msg)
		}

		ctx.Send(message.ReplyWithMessage(ctx.Event.MessageID, message.Text("你用", thingInfos[0].Price, "万,购买了", thingName)))
	})
}

// 获取商店信息
func (sql *storeRepo) getStoreInfo() (thingInfos []storeRecord, err error) {
	sql.Lock()
	defer sql.Unlock()
	thingInfo := storeRecord{}
	err = sql.db.Create("storeRecord", &thingInfo)
	if err != nil {
		return
	}
	count, err := sql.db.Count("storeRecord")
	if err != nil {
		return
	}
	if count == 0 {
		return
	}
	err = sql.db.FindFor("storeRecord", &thingInfo, "ORDER by Name", func() error {
		thingInfos = append(thingInfos, thingInfo)
		return nil
	})
	return
}

func drawStoreEmptyImage() (picImage image.Image, err error) {
	fontData, err := file.GetLazyData(text.BoldFontFile, control.Md5File, true)
	if err != nil {
		return nil, err
	}
	canvas := gg.NewContext(1000, 300)
	// 画底色
	canvas.DrawRectangle(0, 0, 1000, 300)
	canvas.SetRGBA255(243, 255, 255, 255)
	canvas.Fill()
	// 边框框
	canvas.DrawRectangle(0, 0, 1000, 300)
	canvas.SetLineWidth(3)
	canvas.SetRGBA255(0, 0, 0, 255)
	canvas.Stroke()

	canvas.SetColor(color.Black)
	err = canvas.ParseFontFace(fontData, 100)
	if err != nil {
		return nil, err
	}
	textW, textH := canvas.MeasureString("ATRI商店")
	canvas.DrawString("ATRI商店", 10, 10+textH*1.2)
	canvas.DrawLine(10, textH*1.6, textW, textH*1.6)
	canvas.SetLineWidth(3)
	canvas.SetRGBA255(0, 0, 0, 255)
	canvas.Stroke()
	if err = canvas.ParseFontFace(fontData, 50); err != nil {
		return nil, err
	}
	canvas.DrawStringAnchored("当前商店并没有上架任何物品", 500, 10+textH*2+50, 0.5, 0)
	return canvas.Image(), nil
}

func drawStoreInfoImage(storeInfo []storeRecord) (picImage image.Image, err error) {

	fontData, err := file.GetLazyData(text.BoldFontFile, control.Md5File, true)
	if err != nil {
		return nil, err
	}
	canvas := gg.NewContext(1, 1)
	err = canvas.ParseFontFace(fontData, 100)
	if err != nil {
		return nil, err
	}
	titleW, titleH := canvas.MeasureString("价格信息")

	err = canvas.ParseFontFace(fontData, 50)
	if err != nil {
		return nil, err
	}
	_, textH := canvas.MeasureString("高度")
	nameW, _ := canvas.MeasureString("下界合金竿")
	numberW, _ := canvas.MeasureString("10000")
	priceW, _ := canvas.MeasureString("10000")

	bolckW := int(10 + nameW + 50 + numberW + 50 + priceW + 10)
	backY := 10 + int(titleH*2+10)*2 + 10 + len(storeInfo)*int(textH*2) + 10
	canvas = gg.NewContext(bolckW, math.Max(backY, 500))
	// 画底色
	canvas.DrawRectangle(0, 0, float64(bolckW), float64(backY))
	canvas.SetRGBA255(243, 255, 255, 255)
	canvas.Fill()

	// 放字
	canvas.SetColor(color.Black)
	err = canvas.ParseFontFace(fontData, 100)
	if err != nil {
		return nil, err
	}
	canvas.DrawString("兑换商店", 10, 10+titleH*1.2)
	canvas.DrawLine(10, titleH*1.6, titleW, titleH*1.6)
	canvas.SetLineWidth(3)
	canvas.SetRGBA255(0, 0, 0, 255)
	canvas.Stroke()

	textDy := 10 + titleH*1.7
	if err = canvas.ParseFontFace(fontData, 50); err != nil {
		return nil, err
	}

	canvas.DrawStringAnchored("名称", 10+nameW/2, textDy+textH/2, 0.5, 0.5)
	canvas.DrawStringAnchored("数量/个", 10+nameW+10+numberW/2, textDy+textH/2, 0.5, 0.5)
	canvas.DrawStringAnchored("价格/万", 10+nameW+10+numberW+50+priceW/2, textDy+textH/2, 0.5, 0.5)

	for _, info := range storeInfo {
		textDy += textH * 2
		name := info.Name
		numberStr := strconv.Itoa(info.Number)
		pice := info.Price
		canvas.DrawStringAnchored(name, 10+nameW/2, textDy+textH/2, 0.5, 0.5)
		canvas.DrawStringAnchored(numberStr, 10+nameW+10+numberW/2, textDy+textH/2, 0.5, 0.5)
		canvas.DrawStringAnchored(strconv.Itoa(pice), 10+nameW+10+numberW+50+priceW/2, textDy+textH/2, 0.5, 0.5)
	}
	return canvas.Image(), nil
}

// 获取某关键字的数量
func (sql *storeRepo) getNumberFor(uid int64, thing string) (number int, err error) {
	name := strconv.FormatInt(uid, 10) + "Pack"
	sql.Lock()
	defer sql.Unlock()
	userInfo := userPackRecord{}
	err = sql.db.Create(name, &userInfo)
	if err != nil {
		return
	}
	count, err := sql.db.Count(name)
	if err != nil {
		return
	}
	if count == 0 {
		return
	}
	if !sql.db.CanFind(name, "where Name glob '*"+thing+"*'") {
		return
	}
	info := userPackRecord{}
	err = sql.db.FindFor(name, &info, "where Name glob '*"+thing+"*'", func() error {
		number += info.Number
		return nil
	})
	return
}

// 获取商店物品信息
func (sql *storeRepo) getStoreThingInfo(thing string) (thingInfos []storeRecord, err error) {
	sql.Lock()
	defer sql.Unlock()
	thingInfo := storeRecord{}
	err = sql.db.Create("storeRecord", &thingInfo)
	if err != nil {
		return
	}
	count, err := sql.db.Count("storeRecord")
	if err != nil {
		return
	}
	if count == 0 {
		return
	}
	if !sql.db.CanFind("storeRecord", "where Name = '"+thing+"'") {
		return
	}
	err = sql.db.FindFor("storeRecord", &thingInfo, "where Name = '"+thing+"'", func() error {
		thingInfos = append(thingInfos, thingInfo)
		return nil
	})
	return
}

// 获取商品库存
func (sql *storeRepo) checkStoreFor(thing storeRecord) (ok bool, err error) {
	sql.Lock()
	defer sql.Unlock()
	err = sql.db.Create("storeRecord", &thing)
	if err != nil {
		return
	}
	count, err := sql.db.Count("storeRecord")
	if err != nil {
		return
	}
	if count == 0 {
		return false, nil
	}
	if !sql.db.CanFind("storeRecord", "where Name = "+thing.Name) {
		return false, nil
	}
	err = sql.db.Find("storeRecord", &thing, "where Name = "+thing.Name)
	if err != nil {
		return
	}
	if thing.Number < 1 {
		return false, nil
	}
	return true, nil
}

// 更新商店信息
func (sql *storeRepo) updateStoreInfo(thingInfo storeRecord) (err error) {
	sql.Lock()
	defer sql.Unlock()
	err = sql.db.Create("storeRecord", &thingInfo)
	if err != nil {
		return
	}
	return sql.db.Insert("storeRecord", &thingInfo)
}
