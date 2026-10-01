package qntxinbox

import (
	"context"
	"io"
	"slices"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/ses"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	sestypes "github.com/aws/aws-sdk-go-v2/service/sesv2/types"
	"github.com/teranos/errors"
)

// bag is the bucket SES stores received mail in.
type bag interface {
	List(ctx context.Context, prefix string) ([]string, error)
	Get(ctx context.Context, key string) ([]byte, error)
	Move(ctx context.Context, from, to string) error
}

// outgoing is one text mail a User sends.
type outgoing struct {
	From    string
	To      []string
	Subject string
	Text    string
}

type sender interface {
	Send(ctx context.Context, m outgoing) (string, error)
}

// receiptRule is the SES rule whose recipients are the granted addresses.
type receiptRule interface {
	Add(ctx context.Context, address string) error
}

// awsMail reaches S3 and SES with the node's AWS credentials, loaded on every
// call so rotated credentials are read (plugin/grpc/services/mail_ses.go).
type awsMail struct {
	region  string
	bucket  string
	ruleSet string
	rule    string
}

func (a awsMail) config(ctx context.Context) (aws.Config, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(a.region))
	if err != nil {
		return aws.Config{}, errors.Wrap(err, "the node's AWS credentials did not load")
	}
	return cfg, nil
}

func (a awsMail) List(ctx context.Context, prefix string) ([]string, error) {
	cfg, err := a.config(ctx)
	if err != nil {
		return nil, err
	}
	var keys []string
	pages := s3.NewListObjectsV2Paginator(s3.NewFromConfig(cfg), &s3.ListObjectsV2Input{Bucket: aws.String(a.bucket), Prefix: aws.String(prefix)})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return nil, errors.Wrapf(err, "s3://%s/%s did not list", a.bucket, prefix)
		}
		for _, o := range page.Contents {
			keys = append(keys, aws.ToString(o.Key))
		}
	}
	return keys, nil
}

func (a awsMail) Get(ctx context.Context, key string) ([]byte, error) {
	cfg, err := a.config(ctx)
	if err != nil {
		return nil, err
	}
	out, err := s3.NewFromConfig(cfg).GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(a.bucket), Key: aws.String(key)})
	if err != nil {
		return nil, errors.Wrapf(err, "s3://%s/%s did not read", a.bucket, key)
	}
	defer out.Body.Close()
	data, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, errors.Wrapf(err, "s3://%s/%s did not read whole", a.bucket, key)
	}
	return data, nil
}

func (a awsMail) Move(ctx context.Context, from, to string) error {
	cfg, err := a.config(ctx)
	if err != nil {
		return err
	}
	client := s3.NewFromConfig(cfg)
	if _, err := client.CopyObject(ctx, &s3.CopyObjectInput{Bucket: aws.String(a.bucket), CopySource: aws.String(a.bucket + "/" + from), Key: aws.String(to)}); err != nil {
		return errors.Wrapf(err, "s3://%s/%s did not copy to %s", a.bucket, from, to)
	}
	if _, err := client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(a.bucket), Key: aws.String(from)}); err != nil {
		return errors.Wrapf(err, "s3://%s/%s was copied to %s and not deleted", a.bucket, from, to)
	}
	return nil
}

func (a awsMail) Send(ctx context.Context, m outgoing) (string, error) {
	cfg, err := a.config(ctx)
	if err != nil {
		return "", err
	}
	out, err := sesv2.NewFromConfig(cfg).SendEmail(ctx, &sesv2.SendEmailInput{
		FromEmailAddress: aws.String(m.From),
		Destination:      &sestypes.Destination{ToAddresses: m.To},
		Content: &sestypes.EmailContent{Simple: &sestypes.Message{
			Subject: &sestypes.Content{Data: aws.String(m.Subject), Charset: aws.String("UTF-8")},
			Body:    &sestypes.Body{Text: &sestypes.Content{Data: aws.String(m.Text), Charset: aws.String("UTF-8")}},
		}},
	})
	if err != nil {
		return "", errors.Wrapf(err, "SES in %s refused the mail from %s", a.region, m.From)
	}
	return aws.ToString(out.MessageId), nil
}

func (a awsMail) Add(ctx context.Context, address string) error {
	cfg, err := a.config(ctx)
	if err != nil {
		return err
	}
	client := ses.NewFromConfig(cfg)
	described, err := client.DescribeReceiptRule(ctx, &ses.DescribeReceiptRuleInput{RuleSetName: aws.String(a.ruleSet), RuleName: aws.String(a.rule)})
	if err != nil {
		return errors.Wrapf(err, "SES receipt rule %s/%s did not describe", a.ruleSet, a.rule)
	}
	rule := described.Rule
	if slices.Contains(rule.Recipients, address) {
		return nil
	}
	rule.Recipients = append(rule.Recipients, address)
	if _, err := client.UpdateReceiptRule(ctx, &ses.UpdateReceiptRuleInput{RuleSetName: aws.String(a.ruleSet), Rule: rule}); err != nil {
		return errors.Wrapf(err, "SES receipt rule %s/%s did not take %s", a.ruleSet, a.rule, address)
	}
	return nil
}
