package seu

import "testing"

// TestSortLectures 检查排序规则:
// 大类按 已满 -> 可预约 -> 未开放 -> 已预约 -> 已结束, 类内按讲座时间正序。
func TestSortLectures(t *testing.T) {
	items := []Lecture{
		{WID: "ended", Name: "早就截止了", StartAt: "2026-10-01 18:30:00", Status: StatusEnded},
		{WID: "booked", Name: "我约了", StartAt: "2026-10-26 18:30:00", Status: StatusBooked},
		{WID: "full-late", Name: "已满 十一月", StartAt: "2026-11-05 18:30:00", Status: StatusFull},
		{WID: "available", Name: "可预约 十月二十七", StartAt: "2026-10-27 18:30:00", Status: StatusAvailable},
		{WID: "notopen", Name: "未开放 十月三十", StartAt: "2026-10-30 18:30:00", Status: StatusNotOpen},
		{WID: "full-early-b", Name: "B 已满同名时间", StartAt: "2026-10-20 18:30:00", Status: StatusFull},
		{WID: "full-early-a", Name: "A 已满同名时间", StartAt: "2026-10-20 18:30:00", Status: StatusFull},
		{WID: "checked", Name: "已打卡", StartAt: "2026-10-10 18:30:00", Status: StatusCheckedIn},
	}
	sortLectures(items)

	want := []string{
		"full-early-a", // 已满, 10-20, 同时间按名称
		"full-early-b",
		"full-late", // 已满, 11-05
		"available", // 可预约
		"notopen",   // 未开放
		"checked",   // 已打卡 (已预约类), 类内按时间
		"booked",    // 已预约
		"ended",     // 已结束沉底
	}
	for i, wid := range want {
		if items[i].WID != wid {
			got := make([]string, len(items))
			for k := range items {
				got[k] = items[k].WID
			}
			t.Fatalf("排序结果 = %v,\n期望 %v", got, want)
		}
	}
}

// TestLectureSortRank 检查三大类的先后与「其余状态排在后面」。
func TestLectureSortRank(t *testing.T) {
	order := []string{StatusFull, StatusAvailable, StatusNotOpen, StatusBooked, StatusEnded}
	for i := 1; i < len(order); i++ {
		if lectureSortRank(order[i-1]) >= lectureSortRank(order[i]) {
			t.Fatalf("%s 应排在 %s 之前", order[i-1], order[i])
		}
	}
}

// TestSortLecturesEmpty 检查空列表不会 panic。
func TestSortLecturesEmpty(t *testing.T) {
	var items []Lecture
	sortLectures(items)
	if len(items) != 0 {
		t.Fatal("空列表被改动了")
	}
}
