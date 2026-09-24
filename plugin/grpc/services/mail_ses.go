package services

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	sestypes "github.com/aws/aws-sdk-go-v2/service/sesv2/types"
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
	Region string // Empty = the AWS default chain's region.
}

func (t SESTransport) client(ctx context.Context) (*sesv2.Client, string, error) {
	var opts []func(*awsconfig.LoadOptions) error
	if t.Region != "" {
		opts = append(opts, awsconfig.WithRegion(t.Region))
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, "", errors.Wrap(err, "failed to load AWS credentials to reach SES")
	}
	if cfg.Region == "" {
		err := errors.New("no AWS region to reach SES in")
		return nil, "", errors.WithHint(err, "set mail.ses.region in am.toml, or AWS_REGION for the node")
	}
	return sesv2.NewFromConfig(cfg), cfg.Region, nil
}

// Send hands one mail to SES and names the id SES gave it.
func (t SESTransport) Send(ctx context.Context, m OutgoingMail) (string, error) {
	client, region, err := t.client(ctx)
	if err != nil {
		return "", err
	}

	body := &sestypes.Body{}
	if m.HTML != "" {
		body.Html = &sestypes.Content{Data: aws.String(m.HTML), Charset: aws.String("UTF-8")}
	}
	if m.Text != "" {
		body.Text = &sestypes.Content{Data: aws.String(m.Text), Charset: aws.String("UTF-8")}
	}

	out, err := client.SendEmail(ctx, &sesv2.SendEmailInput{
		FromEmailAddress: aws.String(m.From),
		Destination:      &sestypes.Destination{ToAddresses: []string{m.To}},
		Content: &sestypes.EmailContent{
			Simple: &sestypes.Message{
				Subject: &sestypes.Content{Data: aws.String(m.Subject), Charset: aws.String("UTF-8")},
				Body:    body,
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
	account := SESAccount{
		Region:            region,
		ProductionAccess:  out.ProductionAccessEnabled,
		SendingEnabled:    out.SendingEnabled,
		EnforcementStatus: aws.ToString(out.EnforcementStatus),
	}
	if q := out.SendQuota; q != nil {
		account.Max24HourSend = q.Max24HourSend
		account.MaxSendRate = q.MaxSendRate
		account.SentLast24Hours = q.SentLast24Hours
	}
	return account, nil
}
