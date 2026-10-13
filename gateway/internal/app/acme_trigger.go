package app

import (
	"context"
	"fmt"

	"yaginx-gateway/internal/acme"
	"yaginx-gateway/internal/management"
	"yaginx-gateway/internal/management/webdomain"
	"yaginx-gateway/internal/repository"
)

// acmeTrigger bridges the console's "立即申请证书" / status operations to the
// ACME worker. It lives here, not in repository or acme, because those two
// packages must not import each other (acme already imports repository) and
// the management/webdomain handler must stay free of both.
type acmeTrigger struct {
	store  *repository.Postgres
	worker func() *acme.Worker
}

func (t acmeTrigger) RequestNow(ctx context.Context, domainName string) (webdomain.ACMERequestResult, error) {
	worker := t.worker()
	if worker == nil {
		return webdomain.ACMERequestResult{}, fmt.Errorf("ACME worker is not ready")
	}
	domain, isFreeCert, ok, err := t.store.GetACMEDomainByName(ctx, domainName)
	if err != nil {
		return webdomain.ACMERequestResult{}, err
	}
	if !ok {
		return webdomain.ACMERequestResult{}, management.ErrNotFound
	}
	if !isFreeCert {
		return webdomain.ACMERequestResult{Status: "disabled", Message: "请先为该域名开启自动续期开关"}, nil
	}
	result := worker.RequestNow(ctx, domain)
	return webdomain.ACMERequestResult{Status: result.Status, Message: result.Message}, nil
}

func (t acmeTrigger) Status(ctx context.Context, domainName string) (webdomain.ACMEStatusResult, error) {
	status, err := t.store.GetACMEDomainStatus(ctx, domainName)
	if err != nil {
		return webdomain.ACMEStatusResult{}, err
	}
	out := webdomain.ACMEStatusResult{
		Hostname:            status.Hostname,
		CheckedAt:           status.CheckedAt,
		HasOrder:            status.HasOrder,
		OrderStatus:         status.OrderStatus,
		ErrorMessage:        status.ErrorMessage,
		Attempt:             status.Attempt,
		NextAttemptAt:       status.NextAttemptAt,
		CertificateNotAfter: status.CertificateNotAfter,
	}
	if status.Certificate != nil {
		certificate := status.Certificate
		out.Certificate = &webdomain.CertificateStatus{
			Exists: certificate.Exists, Validity: certificate.Validity, IsActive: certificate.IsActive,
			NotBefore: certificate.NotBefore, NotAfter: certificate.NotAfter,
			RemainingSeconds: certificate.RemainingSeconds, RemainingDays: certificate.RemainingDays,
			Fingerprint: certificate.Fingerprint, Issuer: certificate.Issuer, SerialNumber: certificate.SerialNumber,
			DNSNames: certificate.DNSNames, Source: certificate.Source,
		}
	}
	if status.Order != nil {
		order := status.Order
		id := order.ID
		out.ACMEOrder = &webdomain.ACMEOrderStatus{
			Exists: order.Exists, IsInProgress: order.IsInProgress, ID: &id, Status: order.Status,
			Attempt: order.Attempt, CreatedAt: order.CreatedAt, UpdatedAt: order.UpdatedAt,
			NextAttemptAt: order.NextAttemptAt, FinishedAt: order.FinishedAt,
			ErrorClass: order.ErrorClass, ErrorMessage: order.ErrorMessage,
		}
	}
	return out, nil
}
