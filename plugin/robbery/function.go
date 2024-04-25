package robbery

import (
	"github.com/FloatTech/AnimeAPI/wallet"
	"github.com/FloatTech/floatbox/math"
	"github.com/FloatTech/zbputils/ctxext"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
	"math/rand"
	"strconv"
	"time"
)

type cdsheet struct {
	Time     int64 // 时间
	VictimID int64 // 群号
	UserID   int64 // 用户
}

func init() {
	// 打劫功能
	engine.OnRegex(`^打劫\s?(\[CQ:at,qq=(\d+)\]|(\d+))`, getdb).SetBlock(true).Limit(ctxext.LimitByUser).
		Handle(func(ctx *zero.Ctx) {
			uid := ctx.Event.UserID
			fiancee := ctx.State["regex_matched"].([]string)
			victimID, _ := strconv.ParseInt(fiancee[2]+fiancee[3], 10, 64)
			if victimID == uid {
				ctx.Send(message.ReplyWithMessage(ctx.Event.MessageID, message.At(uid), message.Text("不能打劫自己")))
				return
			}

			// 获取CD
			ok, err := police.judgeCD(victimID, uid)
			if err != nil {
				ctx.SendChain(message.Text("[ERROR]:", err))
				return
			}
			if !ok {
				ctx.SendChain(message.Text("你已经打劫过了/对方已经被打劫过了"))
				return
			}

			// 穷人保护
			walletinfo := wallet.GetWalletOf(victimID)
			if walletinfo < 1000 {
				ctx.SendChain(message.Text("对方太穷了！打劫失败"))
				return
			}

			// 判断打劫是否成功
			if rand.Intn(100) > 60 {
				ctx.SendChain(message.Text("打劫失败,罚款1000"))
				err := wallet.InsertWalletOf(victimID, -1000)
				if err != nil {
					ctx.SendChain(message.Text("[ERROR]:罚款失败，钱包坏掉力:\n", err))
					return
				}
				return
			}
			illicitMoney := math.Min(rand.Intn(walletinfo/20)+500, 10000)
			lossMoney := illicitMoney / (rand.Intn(4) + 1)

			// 记录结果
			err = wallet.InsertWalletOf(victimID, -lossMoney)
			if err != nil {
				ctx.SendChain(message.Text("[ERROR]:钱包坏掉力:\n", err))
				return
			}
			err = wallet.InsertWalletOf(uid, +illicitMoney)
			if err != nil {
				ctx.SendChain(message.Text("[ERROR]:打劫失败，脏款掉入虚无\n", err))
				return
			}

			// 写入CD
			err = police.queryCD(victimID, uid)
			if err != nil {
				ctx.SendChain(message.At(uid), message.Text("[ERROR]:犯罪记录写入失败\n", err))
			}

			ctx.SendChain(message.At(uid), message.Text("打劫成功，钱包增加：", illicitMoney, "ATRI币"))
			ctx.SendChain(message.At(victimID), message.Text("保险公司对您进行了赔付，您实际损失：", lossMoney, "ATRI币"))
		})
}

func (sql *criminalRecord) judgeCD(victimID, uid int64) (ok bool, err error) {
	sql.Lock()
	defer sql.Unlock()
	// 创建群表格
	err = sql.db.Create("cdsheet", &cdsheet{})
	if err != nil {
		return false, err
	}
	limitID := "where VictimID is " + strconv.FormatInt(victimID, 10) +
		" or UserID is " + strconv.FormatInt(uid, 10)
	if !sql.db.CanFind("cdsheet", limitID) {
		// 没有记录即不用比较
		return true, nil
	}
	cdinfo := cdsheet{}
	_ = sql.db.Find("cdsheet", &cdinfo, limitID)
	if time.Since(time.Unix(cdinfo.Time, 0)).Hours() > 24 {
		// 如果CD已过就删除
		err = sql.db.Del("cdsheet", limitID)
		return true, err
	}
	return false, nil
}

func (sql *criminalRecord) queryCD(gid int64, uid int64) error {
	sql.Lock()
	defer sql.Unlock()
	return sql.db.Insert("cdsheet", &cdsheet{
		Time:     time.Now().Unix(),
		VictimID: gid,
		UserID:   uid,
	})
}
