package services

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	sestypes "github.com/aws/aws-sdk-go-v2/service/sesv2/types"
	"github.com/teranos/QNTX/internal/named"
	"github.com/teranos/errors"
)

// "ses being enabled for use with email service can be enabled in the am.toml"

// SESTransport sends mail through Amazon SES with the host's AWS credentials.
//
// The configuration is loaded on every call rather than once, the way
// secretref reads SSM: a client held for the life of the node keeps whatever
// credentials it first read, and a host whose credentials rotate would go on
// presenting expired ones.
type SESTransport struct {
	// mail.ses.region. Left out of am.toml, the AWS chain names the region:
	// its own loader reads an empty region as none given.
	Region string
}

func (t SESTransport) client(ctx context.Context) (*sesv2.Client, string, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(t.Region))
	if err != nil {
		return nil, "", errors.Wrap(err, "failed to load AWS credentials to reach SES")
	}
	region, err := named.TextOf("the AWS region to reach SES in", cfg.Region)
	if err != nil {
		return nil, "", errors.WithHint(err, "set mail.ses.region in am.toml, or AWS_REGION for the node")
	}
	return sesv2.NewFromConfig(cfg), region.String(), nil
}

// Send hands one mail to SES and names the id SES gave it.
func (t SESTransport) Send(ctx context.Context, m OutgoingMail) (string, error) {
	client, region, err := t.client(ctx)
	if err != nil {
		return "", err
	}

	// The mail goes out in each body it names.
	body := &sestypes.Body{}
	for _, b := range m.Bodies.All() {
		content := &sestypes.Content{Data: aws.String(b.Content), Charset: aws.String("UTF-8")}
		switch b.Kind {
		case MailHTML:
			body.Html = content
			continue
		case MailText:
			body.Text = content
			continue
		}
		return "", errors.Newf("the mail to %s names a %q body, which SES does not send", m.To, b.Kind)
	}

	// An inline image is referenced from the html by cid:<ContentID>.
	var inline []sestypes.Attachment
	for _, img := range m.Inline {
		inline = append(inline, sestypes.Attachment{
			FileName:                aws.String(img.FileName),
			RawContent:              img.Data,
			ContentType:             aws.String(img.ContentType),
			ContentId:               aws.String(img.ContentID),
			ContentDisposition:      sestypes.AttachmentContentDispositionInline,
			ContentTransferEncoding: sestypes.AttachmentContentTransferEncodingBase64,
		})
	}

	out, err := client.SendEmail(ctx, &sesv2.SendEmailInput{
		FromEmailAddress: aws.String(m.From),
		Destination:      &sestypes.Destination{ToAddresses: []string{m.To}},
		Content: &sestypes.EmailContent{
			Simple: &sestypes.Message{
				Subject:     &sestypes.Content{Data: aws.String(m.Subject), Charset: aws.String("UTF-8")},
				Body:        body,
				Attachments: inline,
			},
		},
	})
	if err != nil {
		return "", errors.Wrapf(err, "SES in %s refused the mail from %s", region, m.From)
	}
	return aws.ToString(out.MessageId), nil
}

// SESAccount is what SES says of the account the node sends from.
type SESAccount struct {
	Region            string  `json:"region"`
	ProductionAccess  bool    `json:"production_access"`
	SendingEnabled    bool    `json:"sending_enabled"`
	EnforcementStatus string  `json:"enforcement_status"`
	Max24HourSend     float64 `json:"max_24_hour_send"`
	MaxSendRate       float64 `json:"max_send_rate"`
	SentLast24Hours   float64 `json:"sent_last_24_hours"`
}

// Account asks SES about the account, as it is now.
func (t SESTransport) Account(ctx context.Context) (SESAccount, error) {
	client, region, err := t.client(ctx)
	if err != nil {
		return SESAccount{}, err
	}
	out, err := client.GetAccount(ctx, &sesv2.GetAccountInput{})
	if err != nil {
		return SESAccount{}, errors.Wrapf(err, "SES in %s did not say what the account is", region)
	}
	// An answer naming no quota is not an account with a quota of 0. The SDK
	// hands the quota as a pointer, so its absence is asked of the pointer,
	// once, and refused.
	quota := out.SendQuota
	if quota == nil {
		return SESAccount{}, errors.Newf("SES in %s answered for the account without its sending quota", region)
	}
	return SESAccount{
		Region:            region,
		ProductionAccess:  out.ProductionAccessEnabled,
		SendingEnabled:    out.SendingEnabled,
		EnforcementStatus: aws.ToString(out.EnforcementStatus),
		Max24HourSend:     quota.Max24HourSend,
		MaxSendRate:       quota.MaxSendRate,
		SentLast24Hours:   quota.SentLast24Hours,
	}, nil
}
