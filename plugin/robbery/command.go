// Package robbery 打劫群友  基于“qqwife”插件魔改
package robbery

import (
	"sync"
	"time"

	ctrl "github.com/FloatTech/zbpctrl"
	control "github.com/FloatTech/zbputils/control"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"

	// 反并发
	"github.com/wdvxdr1123/ZeroBot/extension/single"
	// 数据库
	sql "github.com/FloatTech/sqlite"
	// 画图
	fcext "github.com/FloatTech/floatbox/ctxext"
)

type criminalRecord struct {
	db *sql.Sqlite
	sync.RWMutex
}

var (
	police = &criminalRecord{
		db: &sql.Sqlite{},
	}
	engine = control.AutoRegister(&ctrl.Options[*zero.Ctx]{
		DisableOnDefault: false,
		Brief:            "打劫别人的ATRI币",
		Help: "- 打劫[对方Q号|@对方QQ]\n" +
			"1. 受害者钱包少于1000不能被打劫\n" +
			"2. 打劫成功率 40%\n" +
			"4. 打劫失败罚款1000（钱不够不罚钱）\n" +
			"5. 保险赔付0-80%\n" +
			"6. 打劫成功获得对方0-5%+500的财产（最高1W）\n" +
			"7. 每日可打劫或被打劫一次\n" +
			"8. 打劫失败不计入次数\n",
		PrivateDataFolder: "robbery",
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
	getdb = fcext.DoOnceOnSuccess(func(ctx *zero.Ctx) bool {
		police.db.DBPath = engine.DataFolder() + "criminalRecordDB.db"
		err := police.db.Open(time.Hour)
		if err == nil {
			// 创建CD表
			err = police.db.Create("cdsheet", &cdsheet{})
			if err != nil {
				ctx.SendChain(message.Text("[ERROR]:", err))
				return false
			}
			return true
		}
		ctx.SendChain(message.Text("[ERROR]:", err))
		return false
	})
)
