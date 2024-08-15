// Package atristore 自动贩卖商店
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
	"strings"
	"sync"
	"time"
)

type storeRepo struct {
	db *sql.Sqlite
	sync.RWMutex
}
type storeRecord struct {
	ID     int    `db:"product_id"`     // 商品编号
	Name   string `db:"product_name"`   // 商品名
	Number int    `db:"product_number"` // 商品数量
	Price  int    `db:"product_price"`  // 商品价格
}
type buyRecord struct {
	BuyTime string `gorm:"primaryKey" db:"buy_time"` // 购买时间,主键
	UserID  int64  `db:"user_id"`                    // 购买用户
	Name    string `db:"product_name"`               // 商品名
	Number  int    `db:"buy_number"`                 // 购买数量
	Price   int    `db:"buy_price"`                  // 购买价格
}

func init() {
	dbData := &storeRepo{
		db: &sql.Sqlite{},
	}

	engine := control.AutoRegister(&ctrl.Options[*zero.Ctx]{
		DisableOnDefault: false,
		Brief:            "自动贩卖商店",
		Help: "- #商品列表\n" +
			"- #购买[商品名称]\n" +
			"- #购买[商品名称]空格[商品数量]\n" +
			"- #查看所有订单  (注：需要管理员权限)\n",
		PrivateDataFolder: "atristore",
	}).ApplySingle(single.New(
		single.WithKeyFn(func(ctx *zero.Ctx) int64 { return ctx.Event.GroupID }),
		single.WithPostFn[int64](func(ctx *zero.Ctx) {
			ctx.Send(
				message.ReplyWithMessage(ctx.Event.MessageID,
					message.Text("别着急，超市门口排长队了！"),
				),
			)
		}),
	))

	getdb := fcext.DoOnceOnSuccess(func(ctx *zero.Ctx) bool {
		dbData.db.DBPath = engine.DataFolder() + "atristore.db"
		err := dbData.db.Open(time.Hour)
		if err == nil {
			// 创建商品表
			err = dbData.db.Create("store_Record", &storeRecord{})
			if err != nil {
				ctx.SendChain(message.Text("[ERROR]:", err))
				return false
			}
			// 创建购买记录表
			err = dbData.db.Create("buy_Record", &buyRecord{})
			if err != nil {
				ctx.SendChain(message.Text("[ERROR]:", err))
				return false
			}
			return true
		}
		ctx.SendChain(message.Text("[ERROR]:", err))
		return false
	})

	engine.OnFullMatchGroup([]string{"#商品列表"}, getdb).SetBlock(true).Limit(ctxext.LimitByUser).Handle(func(ctx *zero.Ctx) {
		infos, err := dbData.getStoreInfo()
		if err != nil {
			ctx.SendChain(message.Text("[ERROR]:获取商品信息失败", err))
			return
		}
		var picImage image.Image
		if len(infos) == 0 {
			picImage, err = drawStoreEmptyImage()
		} else {
			picImage, err = drawStoreInfoImage(infos)
		}
		if err != nil {
			ctx.SendChain(message.Text("[ERROR]:生成图片失败", err))
			return
		}
		pic, err := imgfactory.ToBytes(picImage)
		if err != nil {
			ctx.SendChain(message.Text("[ERROR]:图片转换失败", err))
			return
		}
		ctx.SendChain(message.ImageBytes(pic))
	})

	engine.OnFullMatchGroup([]string{"#查看所有订单"}, getdb, zero.AdminPermission).SetBlock(true).Limit(ctxext.LimitByUser).Handle(func(ctx *zero.Ctx) {
		infos, err := dbData.getBugRecordInfo()
		if err != nil {
			ctx.SendChain(message.Text("[ERROR]:获取商品信息失败", err))
			return
		}
		if len(infos) == 0 {
			ctx.SendChain(message.Text("没有购买记录"))
			return
		}

		msg := make(message.Message, 0, len(infos)*12)
		for _, info := range infos {
			msg = append(msg, message.Text("\n-------------------------------------------\n"))
			msg = append(msg,
				message.Text("\n购买时间：  "), message.Text(info.BuyTime),
				message.Text("购买用户："), message.Text(info.UserID),
				message.Text("\n商品名称：  "), message.Text(info.Name),
				message.Text("\n购买数量：  "), message.Text(info.Number),
				message.Text("\n购买价格：  "), message.Text(info.Price))
		}
		ctx.SendChain(message.Text(msg))
	})

	engine.OnFullMatchGroup([]string{"#商品管理"}, getdb, zero.SuperUserPermission).SetBlock(true).Limit(ctxext.LimitByUser).Handle(func(ctx *zero.Ctx) {
		msg := "请输入对应的序号进行管理：\n" +
			"1. 新增商品: \n" +
			"2. 修改商品\n" +
			"3. 删除商品\n" +
			"4. 退出"
		ctx.SendChain(message.Text(msg))
		manageIf := false
		var manageNumber int
		recv, cancel := zero.NewFutureEvent("message", 999, false, zero.RegexRule(`^(取消|\d+)$`), zero.CheckUser(ctx.Event.UserID)).Repeat()
		defer cancel()
		for {
			select {
			case <-time.After(time.Second * 120):
				ctx.Send(
					message.ReplyWithMessage(ctx.Event.MessageID,
						message.Text("等待超时,退出管理功能"),
					),
				)
				return
			case e := <-recv:
				nextcmd := e.Event.Message.String()
				if nextcmd == "取消" {
					ctx.Send(
						message.ReplyWithMessage(ctx.Event.MessageID,
							message.Text("已取消出售"),
						),
					)
					return
				}
				manageNumber, err := strconv.Atoi(e.Event.Message.String())
				if err != nil || manageNumber >= 1 || manageNumber <= 3 {
					ctx.SendChain(message.At(ctx.Event.UserID), message.Text("请输入正确的序号"))
					continue
				}
				manageIf = true
			}
			if manageIf {
				break
			}
		}

		if manageNumber == 1 || manageNumber == 2 {

			// 先输出商品列表，再让用户选择
			infos, err := dbData.getStoreInfo()
			if err != nil {
				ctx.SendChain(message.Text("[ERROR]:获取商品信息失败", err))
				return
			}

			msg := make(message.Message, 0, 3+len(infos))
			for _, info := range infos {
				msg = append(msg, message.Text("序号："), message.Text(info.ID), message.Text("\t商品名："), message.Text(info.Name))
			}
			ctx.SendChain(message.Text("序号：99 \t取消"))

			ctx.SendChain(message.Text("请输入要管理的商品序号：\n", msg))

			manageIf = false
			recv, cancel = zero.NewFutureEvent("message", 999, false, zero.CheckUser(ctx.Event.UserID)).Repeat()
			defer cancel()
			for {
				select {
				case <-time.After(time.Second * 120):
					ctx.Send(
						message.ReplyWithMessage(ctx.Event.MessageID,
							message.Text("等待超时,退出管理功能"),
						),
					)
					return
				case e := <-recv:
					manageId, err := strconv.Atoi(e.Event.Message.String())
					if err != nil || manageId >= len(infos) {
						ctx.SendChain(message.At(ctx.Event.UserID), message.Text("请输入正确的序号"))
						continue
					}
					if manageId == 99 {
						ctx.Send(message.ReplyWithMessage(ctx.Event.MessageID, message.Text("已退出")))
						return
					}
					manageIf = true
				}
				if manageIf {
					break
				}
			}
		}

		if manageNumber == 3 {
			// 删除 manageId
		}

		ctx.SendChain(message.Text("请输入要修改的值，按照（名称 数量 价格/万）格式，用空格分割,或回复“取消”取消\"\n"))
		check := false
		list := []string{"商品名", "数量", "价格"}
		recv, cancel = zero.NewFutureEvent("message", 999, false, zero.RegexRule(`^(取消|\d+ \d+ \d+)$`), zero.CheckUser(ctx.Event.UserID)).Repeat()
		defer cancel()
		for {
			select {
			case <-time.After(time.Second * 120):
				ctx.Send(
					message.ReplyWithMessage(ctx.Event.MessageID,
						message.Text("等待超时,取消合成"),
					),
				)
				return
			case e := <-recv:
				nextcmd := e.Event.Message.String()
				if nextcmd == "取消" {
					ctx.Send(
						message.ReplyWithMessage(ctx.Event.MessageID,
							message.Text("已取消合成"),
						),
					)
					return
				}
				chooseList := strings.Split(nextcmd, " ")
				objectName := chooseList[0]
				objectNumber := chooseList[1]
				objectPice := chooseList[2]

				list = []string{objectName, objectNumber, objectPice}

				check = true
			}
			if check {
				break
			}

		}
	},
	)

	engine.OnRegex(`^#购买\s*([^ ]*)\s*(\d*)$`, getdb).SetBlock(true).Limit(ctxext.LimitByGroup).Handle(func(ctx *zero.Ctx) {
		uid := ctx.Event.UserID
		thingName := ctx.State["regex_matched"].([]string)[1]
		number, _ := strconv.Atoi(ctx.State["regex_matched"].([]string)[2])
		if number == 0 {
			number = 1
		}
		thingInfos, err := dbData.getStoreThingInfo(thingName)
		if err != nil {
			ctx.SendChain(message.Text("[ERROR]:", err))
			return
		}
		if len(thingInfos) == 0 {
			ctx.SendChain(message.Text("当前商店并没有上架该物品"))
			return
		}
		ok, err := dbData.checkStoreFor(thingInfos[0], number)
		if err != nil {
			ctx.SendChain(message.Text("[ERROR]:", err))
			return
		}
		if !ok {
			ctx.SendChain(message.Reply(ctx.Event.MessageID), message.Text("你慢了一步,物品库存不足"))
			return
		}
		money := wallet.GetWalletOf(uid)
		// price 单位为万，计算时时需要*10000
		price := thingInfos[0].Price * number
		if money < price*10000 {
			ctx.SendChain(message.Text("你身上的钱(", money, ")不够支付"))
			return
		}

		ctx.Send(message.ReplyWithMessage(ctx.Event.MessageID, message.Text("你确定花费", price, "W，购买《", thingName, "》?", "\n回答\"是\"或\"否\"")))
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
				nextCmd := e.Event.Message.String()
				if nextCmd == "否" {
					ctx.Send(message.ReplyWithMessage(ctx.Event.MessageID, message.Text("已取消购买")))
					return
				}
				buy = true
			}
			if buy {
				break
			}
		}

		if thingInfos[0].Number < number {
			ctx.Send(message.ReplyWithMessage(ctx.Event.MessageID, message.Text("商店数量不足")))
			return
		}

		// 扣除用户的钱
		err = wallet.InsertWalletOf(uid, -price*10000)
		if err != nil {
			ctx.SendChain(message.Text("[ERROR，扣款失败]:", err))
			return
		}
		// 写入购买记录
		err = dbData.updateBugRecordInfo(uid, thingInfos[0].Name, number, price*10000)
		if err != nil {
			ctx.SendChain(message.Text("[ERROR,购买信息记录失败，请联系管理员]:", err))
			return
		}

		// 扣除商品数量
		thingInfos[0].Number -= number
		err = dbData.updateStoreInfo(thingInfos[0])
		if err != nil {
			ctx.SendChain(message.Text("[ERROR，商品库存更新失败]", err))
			return
		}

		userid := ctx.Event.UserID
		username := ctx.CardOrNickName(userid)
		for _, su := range zero.BotConfig.SuperUsers {
			msg := username + "(QQ:" + strconv.FormatInt(userid, 10) + "),花费：" + strconv.Itoa(price) + "W" + wallet.GetWalletName() + "。购买了" + strconv.Itoa(number) + "个《" + thingName + "》\n请安排发货"
			ctx.SendPrivateMessage(su, msg)
		}

		ctx.Send(message.ReplyWithMessage(ctx.Event.MessageID, message.Text("你用", price, "W", wallet.GetWalletName(), "。购买了", strconv.Itoa(number), "个《"+thingName, "》\n已通知管理员为你发货\n请将此聊天记录截图保存作为凭证")))
	})
}

// 获取商店信息
func (sql *storeRepo) getStoreInfo() (thingInfos []storeRecord, err error) {
	sql.Lock()
	defer sql.Unlock()
	thingInfo := storeRecord{}
	err = sql.db.Create("store_Record", &thingInfo)
	if err != nil {
		return
	}
	count, err := sql.db.Count("store_Record")
	if err != nil {
		return
	}
	if count == 0 {
		return
	}
	err = sql.db.FindFor("store_Record", &thingInfo, "ORDER by product_id", func() error {
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
	textW, textH := canvas.MeasureString("自动贩卖商店")
	canvas.DrawString("自动贩卖商店", 10, 10+textH*1.2)
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
	idW, _ := canvas.MeasureString("编号")
	nameW, _ := canvas.MeasureString("七月萝小红书优惠券2r")
	numberW, _ := canvas.MeasureString("10000")
	priceW, _ := canvas.MeasureString("10000")

	backW := int(10 + idW + 50 + nameW + 50 + numberW + 50 + priceW + 10)
	backY := 10 + int(titleH*2+10)*2 + 10 + len(storeInfo)*int(textH*2) + 10
	canvas = gg.NewContext(backW, math.Max(backY, 500))
	// 画底色
	canvas.DrawRectangle(0, 0, float64(backW), float64(backY))
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
	canvas.DrawStringAnchored("编号", 10+idW/2, textDy+textH/2, 0.5, 0.5)
	canvas.DrawStringAnchored("名称", 10+idW/2+40+nameW/2, textDy+textH/2, 0.5, 0.5)
	canvas.DrawStringAnchored("数量", 10+idW/2+40+nameW+40+numberW/2, textDy+textH/2, 0.5, 0.5)
	canvas.DrawStringAnchored("价格/万", 10+idW/2+40+nameW+40+numberW+10+priceW/2, textDy+textH/2, 0.5, 0.5)

	for _, info := range storeInfo {
		textDy += textH * 2
		name := info.Name
		numberStr := strconv.Itoa(info.Number)
		price := info.Price
		id := info.ID
		canvas.DrawStringAnchored(strconv.Itoa(id), 10+idW/2, textDy+textH/2, 0.5, 0.5)
		canvas.DrawStringAnchored(name, 10+idW/2+40+nameW/2, textDy+textH/2, 0.5, 0.5)
		canvas.DrawStringAnchored(numberStr, 10+idW/2+40+nameW+40+numberW/2, textDy+textH/2, 0.5, 0.5)
		canvas.DrawStringAnchored(strconv.Itoa(price), 10+idW/2+40+nameW+40+numberW+10+priceW/2, textDy+textH/2, 0.5, 0.5)
	}
	return canvas.Image(), nil
}

/*// 获取某关键字的数量
func (sql *storeRepo) getNumberFor(uid int64, thing string) (number int, err error) {
	name := strconv.FormatInt(uid, 10) + "Pack"
	sql.Lock()
	defer sql.Unlock()
	userInfo := buyRecord{}
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
	info := buyRecord{}
	err = sql.db.FindFor(name, &info, "where Name glob '*"+thing+"*'", func() error {
		number += info.Number
		return nil
	})
	return
}*/

// 获取商店物品信息
func (sql *storeRepo) getStoreThingInfo(thing string) (thingInfos []storeRecord, err error) {
	sql.Lock()
	defer sql.Unlock()
	thingInfo := storeRecord{}
	err = sql.db.Create("store_Record", &thingInfo)
	if err != nil {
		return
	}
	count, err := sql.db.Count("store_Record")
	if err != nil {
		return
	}
	if count == 0 {
		return
	}
	if !sql.db.CanFind("store_Record", "where product_name = '"+thing+"'") {
		return
	}
	err = sql.db.FindFor("store_Record", &thingInfo, "where product_name = '"+thing+"'", func() error {
		thingInfos = append(thingInfos, thingInfo)
		return nil
	})
	return
}

// 获取商品库存
func (sql *storeRepo) checkStoreFor(thing storeRecord, number int) (ok bool, err error) {
	sql.Lock()
	defer sql.Unlock()
	err = sql.db.Create("store_Record", &thing)
	if err != nil {
		return
	}
	limitID := "where product_id = " + strconv.Itoa(thing.ID)
	if !sql.db.CanFind("store_Record", limitID) {
		return false, nil
	}
	err = sql.db.Find("store_Record", &thing, limitID)
	if err != nil {
		return
	}
	if thing.Number < number {
		return false, nil
	}
	return true, nil
}

// 更新商店信息
func (sql *storeRepo) updateStoreInfo(thingInfo storeRecord) (err error) {
	sql.Lock()
	defer sql.Unlock()
	err = sql.db.Create("store_Record", &thingInfo)
	if err != nil {
		return
	}
	return sql.db.Insert("store_Record", &thingInfo)
}

// 更新商店信息
func (sql *storeRepo) updateBugRecordInfo(userID int64, productName string, buyNumber int, buyPrice int) (err error) {
	sql.Lock()
	defer sql.Unlock()
	thingInfo := buyRecord{}
	err = sql.db.Create("buy_Record", &thingInfo)
	if err != nil {
		return
	}
	return sql.db.Insert("buy_Record", &buyRecord{
		UserID:  userID,
		Name:    productName,
		Number:  buyNumber,
		Price:   buyPrice,
		BuyTime: time.Now().Format("2006-01-02 15:04:05"),
	})
}

// 获取购买记录
func (sql *storeRepo) getBugRecordInfo() (thingInfos []buyRecord, err error) {
	sql.Lock()
	defer sql.Unlock()
	thingInfo := buyRecord{}
	err = sql.db.Create("buy_Record", &thingInfo)
	if err != nil {
		return
	}
	count, err := sql.db.Count("buy_Record")
	if err != nil {
		return
	}
	if count == 0 {
		return
	}
	err = sql.db.FindFor("buy_Record", &thingInfo, "ORDER by buy_time", func() error {
		thingInfos = append(thingInfos, thingInfo)
		return nil
	})
	return
}
