package seu

import (
	"strings"
	"time"
)

// 讲座状态 (与前端 src/store.ts 里的状态推导保持一致)。
const (
	StatusAvailable = "available"  // 可预约
	StatusFull      = "full"       // 已满
	StatusNotOpen   = "not_open"   // 未开放
	StatusEnded     = "ended"      // 已结束
	StatusBooked    = "booked"     // 已预约
	StatusCheckedIn = "checked_in" // 已打卡
	StatusDefaulted = "defaulted"  // 已违约
	StatusCancelled = "cancelled"  // 已取消
)

// UserInfo 是登录用户信息。
type UserInfo struct {
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	ClientIP    string `json:"clientIp"`
	LoginTime   string `json:"loginTime"`
	Online      bool   `json:"online"`
}

// Lecture 是一讲素质讲座。
type Lecture struct {
	WID         string `json:"wid"`
	Name        string `json:"name"`
	Series      string `json:"series"`      // JZXL_DISPLAY, 所属系列
	Category    string `json:"category"`    // JZJB, 讲座级别/类别
	StartAt     string `json:"startAt"`     // JZSJ
	EndAt       string `json:"endAt"`       // HDJSSJ
	Venue       string `json:"venue"`       // JZDD
	Speaker     string `json:"speaker"`     // ZJR
	SpeakerBio  string `json:"speakerBio"`  // ZJRJS
	Intro       string `json:"intro"`       // JZJS
	Organizer   string `json:"organizer"`   // ZBF
	Capacity    int    `json:"capacity"`    // HDZRS
	Booked      int    `json:"booked"`      // YYRS
	BookStartAt string `json:"bookStartAt"` // YYKSSJ
	BookEndAt   string `json:"bookEndAt"`   // YYJSSJ
	ServerTime  string `json:"serverTime"`  // XTDQSJ
	Published   bool   `json:"published"`   // FBZT != "-1"
	Joined      bool   `json:"joined"`      // 已有预约 (YY_WID 非空)
	CheckedIn   bool   `json:"checkedIn"`   // SFDK == "1"
	Defaulted   bool   `json:"defaulted"`   // SFWY == "1"
	Status      string `json:"status"`
}

// Remain 返回剩余名额 (可能为负, 表示超额)。
func (l Lecture) Remain() int { return l.Capacity - l.Booked }

// MyActivity 是「我的预约」里的一条记录。
type MyActivity struct {
	WID        string `json:"wid"` // HD_WID, 与 Lecture.WID 相同
	RecordWID  string `json:"recordWid"`
	Name       string `json:"name"`
	Series     string `json:"series"`
	StartAt    string `json:"startAt"`
	EndAt      string `json:"endAt"`
	Venue      string `json:"venue"`
	Speaker    string `json:"speaker"`
	BookedAt   string `json:"bookedAt"`  // YYSJ
	Code       string `json:"code"`      // YYM, 现场预约码
	CheckInAt  string `json:"checkInAt"` // DKSJ
	CheckedIn  bool   `json:"checkedIn"`
	Defaulted  bool   `json:"defaulted"`
	Published  bool   `json:"published"`
	ServerTime string `json:"serverTime"`
	Status     string `json:"status"`
}

// Notice 是一条通知公告。
type Notice struct {
	WID     string `json:"wid"`
	Title   string `json:"title"` // BT
	Body    string `json:"body"`  // NR
	Date    string `json:"date"`  // FBSJ
	Top     bool   `json:"top"`   // SFZD == "1"
	Read    bool   `json:"read"`  // SFYD == "1"
	Publish bool   `json:"publish"`
}

// Result 是预约/取消这类业务操作的结果。
type Result struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	// Kind 便于前端做差异化提示: ok / limited / vcode / closed / other
	Kind string `json:"kind"`
}

// --- 解析辅助 ---------------------------------------------------------------

func fieldString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	return rawString(m[key])
}

func fieldInt(m map[string]any, key string) int {
	text := fieldString(m, key)
	n := 0
	negative := false
	for i, ch := range text {
		if i == 0 && ch == '-' {
			negative = true
			continue
		}
		if ch < '0' || ch > '9' {
			return n
		}
		n = n*10 + int(ch-'0')
	}
	if negative {
		return -n
	}
	return n
}

func fieldBool(m map[string]any, key string) bool {
	return truthy(m[key])
}

// campusZone 是教务系统时间字段所用的时区(北京时间)。
// 这里用固定偏移而不是 time.Local: 服务器/Docker 镜像里通常没有 tzdata,
// time.Local 会退回 UTC, 会让「未开放/已结束」判断和显示的时间整体偏 8 小时。
var campusZone = time.FixedZone("CST", 8*60*60)

// Now 返回北京时间的当前时刻, 用于日志/抓取时间等展示字段。
func Now() time.Time { return time.Now().In(campusZone) }

// ParseTime 解析接口的时间字段, 支持到秒/分/日三种精度。
func ParseTime(value string) (time.Time, bool) {
	text := strings.TrimSpace(value)
	if text == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{TimeLayout, "2006-01-02 15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, text, campusZone); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// ToLecture 把服务端原始 JSON 转成 Lecture。
func ToLecture(m map[string]any, now time.Time) Lecture {
	lecture := Lecture{
		WID:         fieldString(m, "WID"),
		Name:        fieldString(m, "JZMC"),
		Series:      fieldString(m, "JZXL_DISPLAY"),
		Category:    fieldString(m, "JZJB"),
		StartAt:     fieldString(m, "JZSJ"),
		EndAt:       fieldString(m, "HDJSSJ"),
		Venue:       fieldString(m, "JZDD"),
		Speaker:     fieldString(m, "ZJR"),
		SpeakerBio:  fieldString(m, "ZJRJS"),
		Intro:       fieldString(m, "JZJS"),
		Organizer:   fieldString(m, "ZBF"),
		Capacity:    fieldInt(m, "HDZRS"),
		Booked:      fieldInt(m, "YYRS"),
		BookStartAt: fieldString(m, "YYKSSJ"),
		BookEndAt:   fieldString(m, "YYJSSJ"),
		ServerTime:  fieldString(m, "XTDQSJ"),
		Published:   fieldString(m, "FBZT") != "-1",
		Joined:      fieldString(m, "YY_WID") != "",
		CheckedIn:   fieldString(m, "SFDK") == "1",
		Defaulted:   fieldString(m, "SFWY") == "1",
	}
	lecture.Status = DeriveStatus(lecture, now)
	return lecture
}

// ToMyActivity 把「我的预约」原始 JSON 转成 MyActivity。
func ToMyActivity(m map[string]any) MyActivity {
	item := MyActivity{
		WID:        fieldString(m, "HD_WID"),
		RecordWID:  fieldString(m, "WID"),
		Name:       fieldString(m, "JZMC"),
		Series:     fieldString(m, "JZXL_DISPLAY"),
		StartAt:    fieldString(m, "JZSJ"),
		EndAt:      fieldString(m, "HDJSSJ"),
		Venue:      fieldString(m, "JZDD"),
		Speaker:    fieldString(m, "ZJR"),
		BookedAt:   fieldString(m, "YYSJ"),
		Code:       fieldString(m, "YYM"),
		CheckInAt:  fieldString(m, "DKSJ"),
		CheckedIn:  fieldString(m, "SFDK") == "1",
		Defaulted:  fieldString(m, "SFWY") == "1",
		Published:  fieldString(m, "FBZT") != "-1",
		ServerTime: fieldString(m, "XTDQSJ"),
	}
	switch {
	case !item.Published:
		item.Status = StatusCancelled
	case item.Defaulted:
		item.Status = StatusDefaulted
	case item.CheckedIn:
		item.Status = StatusCheckedIn
	default:
		item.Status = StatusBooked
	}
	return item
}

// ToNotice 把通知公告原始 JSON 转成 Notice。
func ToNotice(m map[string]any) Notice {
	return Notice{
		WID:     fieldString(m, "WID"),
		Title:   fieldString(m, "BT"),
		Body:    fieldString(m, "NR"),
		Date:    fieldString(m, "FBSJ"),
		Top:     fieldString(m, "SFZD") == "1",
		Read:    fieldString(m, "SFYD") == "1",
		Publish: fieldString(m, "SFFB") != "0",
	}
}

// DeriveStatus 根据服务器时间推导讲座状态。
func DeriveStatus(l Lecture, now time.Time) string {
	if !l.Published {
		return StatusCancelled
	}
	if l.Joined {
		switch {
		case l.Defaulted:
			return StatusDefaulted
		case l.CheckedIn:
			return StatusCheckedIn
		default:
			return StatusBooked
		}
	}
	if start, ok := ParseTime(l.BookStartAt); ok && now.Before(start) {
		return StatusNotOpen
	}
	if end, ok := ParseTime(l.BookEndAt); ok && now.After(end) {
		return StatusEnded
	}
	if l.Capacity > 0 && l.Booked >= l.Capacity {
		return StatusFull
	}
	return StatusAvailable
}

// ServerClock 从列表数据里提取服务器当前时间, 没有则退回本机时间。
func ServerClock(lectures []Lecture) time.Time {
	for _, l := range lectures {
		if t, ok := ParseTime(l.ServerTime); ok {
			return t
		}
	}
	return time.Now()
}
