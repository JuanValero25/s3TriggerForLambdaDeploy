package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"github.com/aws/aws-lambda-go/events"
	lambdaHandler "github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/lambda"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"io"
	"io/ioutil"
	"log"
	"os"
	"strings"
	"time"
)

const s3StaticValue = "https://s3.amazonaws.com/"

type lambdaConfig struct {
	Function    string            `json:"Function"`
	Memory      int64             `json:"Memory"`
	Handler     string            `json:"Handler"`
	Runtime     string            `json:"Runtime"`
	CodeUri     string            `json:"CodeUri"`
	Description string            `json:"Description"`
	Role        string            `json:"Role"`
	Timeout     int64             `json:"Timeout"`
	Environment map[string]string `json:"Environment"`
}

func handler(ctx context.Context, s3Event events.S3Event) {
	file, err := os.Create("/tmp/main.zip")
	if err != nil {
		fmt.Println("Unable to open file ", err)
	}
	sess, _ := session.NewSession(&aws.Config{
		Region: aws.String("us-east-1")},
	)
	start := time.Now()
	downloader := s3manager.NewDownloader(sess)
	for _, record := range s3Event.Records {
		s3object := record.S3
		downloader.Download(file,
			&s3.GetObjectInput{
				Bucket: aws.String(s3object.Bucket.Name),
				Key:    aws.String(s3object.Object.Key),
			})
		elapsed := time.Since(start)
		log.Printf("download took  %s", elapsed)
		lambdaConfigs := Unzip()
		if existLambda(lambdaConfigs) {
			updateLambda(s3object, lambdaConfigs)
			return
		}
		ExampleLambda_CreateFunction(s3object, lambdaConfigs)
	}

}

func main() {
	lambdaHandler.Start(handler)
}

func ExampleLambda_CreateFunction(s3 events.S3Entity, conf *lambdaConfig) {
	svc := lambda.New(session.New())
	input := &lambda.CreateFunctionInput{
		Code: &lambda.FunctionCode{
			S3Bucket: aws.String(s3.Bucket.Name),
			S3Key:    aws.String(s3.Object.Key),
		},
		Description:  aws.String(conf.Description),
		FunctionName: aws.String(conf.Function),
		Handler:      aws.String("main"),
		MemorySize:   aws.Int64(conf.Memory),
		Publish:      aws.Bool(true),
		Role:         aws.String(conf.Role),
		Runtime:      aws.String(lambda.RuntimeGo1X),
		Timeout:      aws.Int64(conf.Timeout),
		Environment: &lambda.Environment{
			Variables: aws.StringMap(conf.Environment),
		},
	}

	result, err := svc.CreateFunction(input)
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			case lambda.ErrCodeServiceException:
				fmt.Println(lambda.ErrCodeServiceException, aerr.Error())
			case lambda.ErrCodeInvalidParameterValueException:
				fmt.Println(lambda.ErrCodeInvalidParameterValueException, aerr.Error())
			case lambda.ErrCodeResourceNotFoundException:
				fmt.Println(lambda.ErrCodeResourceNotFoundException, aerr.Error())
			case lambda.ErrCodeResourceConflictException:
				fmt.Println(lambda.ErrCodeResourceConflictException, aerr.Error())
			case lambda.ErrCodeTooManyRequestsException:
				fmt.Println(lambda.ErrCodeTooManyRequestsException, aerr.Error())
			case lambda.ErrCodeCodeStorageExceededException:
				fmt.Println(lambda.ErrCodeCodeStorageExceededException, aerr.Error())
			default:
				fmt.Println(aerr.Error())
			}
		} else {
			fmt.Println(err.Error())
		}
		return
	}

	fmt.Println(result)
}

func existLambda(conf *lambdaConfig) bool {
	fmt.Println("entrando a la funcion existeLambda")
	svc := lambda.New(session.New())
	input := &lambda.GetFunctionInput{FunctionName: aws.String(conf.Function)}
	_, err := svc.GetFunction(input)
	fmt.Println("el error chequindo si existe algun lambda con el nombre ", err)
	if err == nil {
		return true
	}
	return false
}

func updateLambda(s3 events.S3Entity, conf *lambdaConfig) {
	fmt.Println("entrado al update lambda")
	svc := lambda.New(session.New())
	input := &lambda.UpdateFunctionCodeInput{FunctionName: aws.String(conf.Function), S3Key: aws.String(s3.Object.Key), S3Bucket: aws.String(s3.Bucket.Name), Publish: aws.Bool(true)}
	_, err := svc.UpdateFunctionCode(input)
	if err != nil {
		fmt.Println("error del resultado de update function ", err)
	}

}

func Unzip() (c *lambdaConfig) {
	var cofigs lambdaConfig
	r, err := zip.OpenReader("/tmp/main.zip")
	if err != nil {
		fmt.Println(err)
		return nil
	}
	for _, f := range r.File {
		if strings.Contains(f.Name, "json") {
			rc, err := f.Open()
			cofigs.getConf(rc)
			if err != nil {
				fmt.Println(" config parsed finished ", err)
				return nil
			}
		}
	}
	return &cofigs
}

func (c *lambdaConfig) getConf(rc io.ReadCloser) *lambdaConfig {
	jsonFile, err := ioutil.ReadAll(rc)
	if err != nil {
		log.Printf("error with json", err)
	}
	json.Unmarshal(jsonFile, &c)
	return c
}
