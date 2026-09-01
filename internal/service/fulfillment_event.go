package service

import (
	"encoding/json"
	"log"
	"strings"
	"time"

	"yujixinjiang/backend/internal/model"

	"gorm.io/gorm"
)

type FulfillmentEventInput struct {
	SubjectType string
	SubjectID   uint64
	EventCode   string
	ActorRole   string
	ActorID     *uint64
	Title       string
	Detail      map[string]interface{}
}

type FulfillmentEventView struct {
	ID          uint64                 `json:"id"`
	SubjectType string                 `json:"subject_type"`
	SubjectID   uint64                 `json:"subject_id"`
	EventCode   string                 `json:"event_code"`
	ActorRole   string                 `json:"actor_role"`
	ActorID     *uint64                `json:"actor_id,omitempty"`
	Title       string                 `json:"title"`
	Detail      map[string]interface{} `json:"detail,omitempty"`
	DetailText  string                 `json:"detail_text,omitempty"`
	CreatedAt   time.Time              `json:"created_at"`
}

type FulfillmentEventService struct {
	DB *gorm.DB
}

// AppendFulfillmentEventInTx 在事务内追加事件；失败仅打日志，不阻断主流程。
func AppendFulfillmentEventInTx(tx *gorm.DB, in FulfillmentEventInput) {
	if tx == nil || in.SubjectType == "" || in.SubjectID == 0 || in.EventCode == "" {
		return
	}
	if in.ActorRole == "" {
		in.ActorRole = model.FulfillmentActorSystem
	}
	if in.Title == "" {
		in.Title = in.EventCode
	}
	var detail json.RawMessage
	if len(in.Detail) > 0 {
		b, err := json.Marshal(in.Detail)
		if err != nil {
			log.Printf("[fulfillment_event] marshal detail: %v", err)
		} else {
			detail = b
		}
	}
	ev := model.FulfillmentEvent{
		SubjectType: in.SubjectType,
		SubjectID:   in.SubjectID,
		EventCode:   in.EventCode,
		ActorRole:   in.ActorRole,
		ActorID:     in.ActorID,
		Title:       in.Title,
		Detail:      detail,
		CreatedAt:   time.Now(),
	}
	if err := tx.Create(&ev).Error; err != nil {
		log.Printf("[fulfillment_event] create failed: %v", err)
	}
}

// AppendFulfillmentEvent 独立连接写入（非事务路径）。
func AppendFulfillmentEvent(db *gorm.DB, in FulfillmentEventInput) {
	if db == nil {
		return
	}
	AppendFulfillmentEventInTx(db, in)
}

func (s *FulfillmentEventService) List(subjectType string, subjectID uint64, limit int) ([]FulfillmentEventView, error) {
	if limit < 1 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	var rows []model.FulfillmentEvent
	err := s.DB.Model(&model.FulfillmentEvent{}).
		Where("subject_type = ? AND subject_id = ?", subjectType, subjectID).
		// 按事件发生时间排序：回补的历史事件 id 顺序与 created_at 不一致，不能按 id 排
		Order("created_at ASC, id ASC").Limit(limit).Find(&rows).Error
	if err != nil {
		if isTableMissing(err) {
			return []FulfillmentEventView{}, nil
		}
		return nil, err
	}
	out := make([]FulfillmentEventView, 0, len(rows))
	for i := range rows {
		out = append(out, toFulfillmentEventView(rows[i]))
	}
	return out, nil
}

func toFulfillmentEventView(e model.FulfillmentEvent) FulfillmentEventView {
	v := FulfillmentEventView{
		ID:          e.ID,
		SubjectType: e.SubjectType,
		SubjectID:   e.SubjectID,
		EventCode:   e.EventCode,
		ActorRole:   e.ActorRole,
		ActorID:     e.ActorID,
		Title:       e.Title,
		CreatedAt:   e.CreatedAt,
	}
	if len(e.Detail) > 0 {
		var m map[string]interface{}
		if json.Unmarshal(e.Detail, &m) == nil {
			v.Detail = m
			if reason, ok := m["reason"].(string); ok && reason != "" {
				v.DetailText = reason
			} else if msg, ok := m["message"].(string); ok && msg != "" {
				v.DetailText = msg
			}
		}
	}
	return v
}

func isTableMissing(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "fulfillment_event") &&
		(strings.Contains(msg, "doesn't exist") || strings.Contains(msg, "Error 1146") || strings.Contains(msg, "1146"))
}
