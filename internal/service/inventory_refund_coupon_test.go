package service

import (
	"testing"

	"yujixinjiang/backend/internal/model"
)

func TestPlanOrderItemRefundWithMeta_UsesPayWeightedUnit(t *testing.T) {
	// 优惠后单价 8、实付 8、数量 1 → 应按 8 退
	meta := &orderRefundMeta{
		UnitPrice: 8,
		PayAmount: 8,
		PayStatus: model.PayStatusPaid,
	}
	qty, amount, err := planOrderItemRefundWithMeta(meta, 1)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if qty != 1 || amount != 8 {
		t.Fatalf("got qty=%d amount=%v want 1 / 8", qty, amount)
	}
}

func TestPlanOrderItemRefundWithMeta_ZeroPayAllowsQty(t *testing.T) {
	meta := &orderRefundMeta{
		UnitPrice: 0,
		PayAmount: 0,
		PayStatus: model.PayStatusPaid,
	}
	qty, amount, err := planOrderItemRefundWithMeta(meta, 2)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if qty != 2 || amount != 0 {
		t.Fatalf("got qty=%d amount=%v want 2 / 0", qty, amount)
	}
	// 已标退款的零元单仍可退剩余件数
	meta.PayStatus = model.PayStatusRefunded
	qty, amount, err = planOrderItemRefundWithMeta(meta, 1)
	if err != nil {
		t.Fatalf("refunded zero-pay: %v", err)
	}
	if qty != 1 || amount != 0 {
		t.Fatalf("got qty=%d amount=%v want 1 / 0", qty, amount)
	}
}

func TestRefundAmountAcceptable_ZeroPay(t *testing.T) {
	meta := &orderRefundMeta{PayAmount: 0}
	if !refundAmountAcceptable(meta, 0) {
		t.Fatal("zero-pay amount 0 should be acceptable")
	}
	meta.PayAmount = 10
	if refundAmountAcceptable(meta, 0) {
		t.Fatal("positive-pay amount 0 should be rejected")
	}
}
