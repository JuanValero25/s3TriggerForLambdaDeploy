package main

import (
	"archive/zip"
	"bufio"
	"context"
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
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

func handler(ctx context.Context, s3Event events.S3Event) (err error) {
	var file *os.File
	file, err = os.Create("/tmp/main.zip")
	if err != nil {
		fmt.Println("Unable to open file ", err)
	}
	sess, _ := session.NewSession(&aws.Config{
		Region: aws.String("us-east-1")},
	)
	start := time.Now()
	downloader := s3manager.NewDownloader(sess)
	var lambdaFunctionConf *lambda.FunctionConfiguration
	for _, record := range s3Event.Records {
		s3object := record.S3
		_, _ = downloader.Download(file,
			&s3.GetObjectInput{
				Bucket: aws.String(s3object.Bucket.Name),
				Key:    aws.String(s3object.Object.Key),
			})
		elapsed := time.Since(start)
		log.Printf("download took  %s", elapsed)
		lambdaConfigs, err := Unzip(s3object)
		if err != nil {
			return err
		}

		if existLambda(lambdaConfigs) {
			lambdaFunctionConf = updateLambda(s3object, lambdaConfigs)

		} else {
			lambdaFunctionConf = CreateLambdaFunction(lambdaConfigs)
		}
		createAlias(getStageByS3Key(s3object.Object.Key), lambdaFunctionConf)

	}
	os.Remove("/tmp/main.zip")
	return err
}

func main() {
	lambdaHandler.Start(handler)
}

func CreateLambdaFunction(lambdaConfiguration *lambda.CreateFunctionInput) (result *lambda.FunctionConfiguration) {
	fmt.Println("entrado CreateFunction")

	svc := lambda.New(session.New(&aws.Config{
		Region: aws.String("us-east-1")}))
	result, err := svc.CreateFunction(lambdaConfiguration)
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
	return
}

func createAlias(stage string, functionConfig *lambda.FunctionConfiguration) {
	svc := lambda.New(session.New(&aws.Config{
		Region: aws.String("us-east-1")}))
	aliasInput := &lambda.UpdateAliasInput{
		FunctionName:    functionConfig.FunctionName,
		FunctionVersion: functionConfig.Version,
		Name:            aws.String(stage),
	}
	getAliasAliasInput := &lambda.GetAliasInput{
		FunctionName: functionConfig.FunctionName,
		Name:         aws.String(stage),
	}
	_, err := svc.GetAlias(getAliasAliasInput)

	if err != nil {
		fmt.Println("creating alias")
		createAliasInput := &lambda.CreateAliasInput{
			FunctionName:    functionConfig.FunctionName,
			FunctionVersion: functionConfig.Version,
			Name:            aws.String(stage),
		}
		_, err := svc.CreateAlias(createAliasInput)
		if err != nil {
			fmt.Println(err)
		}
		return
	}
	fmt.Println("updating alias")
	_, err = svc.UpdateAlias(aliasInput)

	if err != nil {
		fmt.Println("error creating alias : ", err)
	}
}

func existLambda(conf *lambda.CreateFunctionInput) bool {
	fmt.Println("entrando a la funcion existeLambda")
	svc := lambda.New(session.New(&aws.Config{
		Region: aws.String("us-east-1")}))
	//arn:aws:lambda:us-east-1:655622384061:function:
	input := &lambda.GetFunctionInput{FunctionName: aws.String("arn:aws:lambda:us-east-1:655622384061:function:" + *conf.FunctionName)}
	_, err := svc.GetFunction(input)
	if err == nil {
		fmt.Println("el error chequindo si existe algun lambda con el nombre ", err)
		return true
	}
	return false
}

func getStageByS3Key(s3Key string) string {
	stage := strings.Split(s3Key, "/")[0]
	if stage == "develop" {
		return "DEV"
	}
	return "PROD"

}

func updateLambda(s3 events.S3Entity, conf *lambda.CreateFunctionInput) (result *lambda.FunctionConfiguration) {
	fmt.Println("entrado al update lambda")
	newSession, err := session.NewSession(&aws.Config{
		Region: aws.String("us-east-1")})
	svc := lambda.New(newSession)
	input := &lambda.UpdateFunctionCodeInput{
		FunctionName: conf.FunctionName,
		Publish:      conf.Publish,
		S3Bucket:     aws.String(s3.Bucket.Name),
		S3Key:        aws.String(s3.Object.Key),
	}
	result, err = svc.UpdateFunctionCode(input)
	if err != nil {
		fmt.Println("error del resultado de update function ", err)
	}

	result, err = svc.UpdateFunctionConfiguration(&lambda.UpdateFunctionConfigurationInput{
		Description:  conf.Description,
		FunctionName: conf.FunctionName,
		Handler:      conf.Handler,
		MemorySize:   conf.MemorySize,
		Role:         conf.Role,
		Runtime:      conf.Runtime,
		Timeout:      conf.Timeout,
		VpcConfig:    conf.VpcConfig,
	})
	if err != nil {
		fmt.Println("error del resultado de update function ", err)
	}
	return
}

func Unzip(s3 events.S3Entity) (lambdaConfiguration *lambda.CreateFunctionInput, err error) {
	var r *zip.ReadCloser
	r, err = zip.OpenReader("/tmp/main.zip")
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, f := range r.File {
		if strings.Contains(f.Name, "lambda.properties") {
			rc, err := f.Open()
			lambdaConfiguration, err = ReadPropertiesFile(rc, s3)
			if err != nil {
				fmt.Println(" config parsed finished ", err)
				return lambdaConfiguration, err
			}
			return lambdaConfiguration, err
		}
	}
	return lambdaConfiguration, err
}

type AppConfigProperties map[string]string

func ReadPropertiesFile(file io.Reader, s3 events.S3Entity) (lambdaConfiguration *lambda.CreateFunctionInput, err error) {
	config := AppConfigProperties{}

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if equal := strings.Index(line, "="); equal >= 0 {
			if key := strings.TrimSpace(line[:equal]); len(key) > 0 {
				value := ""
				if len(line) > equal {
					value = strings.TrimSpace(line[equal+1:])
				}
				config[key] = value
			}
		}
	}

	if err = scanner.Err(); err != nil {
		log.Fatal(err)
		return lambdaConfiguration, err
	}
	var memory int64
	memory, err = strconv.ParseInt(config["MEMORY_SIZE"], 10, 64)
	if err != nil {
		fmt.Println("error parsing properties", err)
		return
	}
	var Timeout int64
	Timeout, err = strconv.ParseInt(config["TIMEOUT"], 10, 64)
	if err != nil {
		fmt.Println("error parsing properties", err)
		return
	}
	var SecurityGroups []*string
	var SubNetsID []*string
	for _, secGrup := range strings.Split(config["SECURITY_GROUPS_ID"], ",") {
		SecurityGroups = append(SecurityGroups, &secGrup)
	}

	for _, subNets := range strings.Split(config["SUB_NETS_ID"], ",") {
		SecurityGroups = append(SubNetsID, &subNets)
	}

	publishConfig, err := strconv.ParseBool(config["PUBLISH"])
	lambdaConfiguration = &lambda.CreateFunctionInput{
		Code: &lambda.FunctionCode{
			S3Bucket: aws.String(s3.Bucket.Name),
			S3Key:    aws.String(s3.Object.Key),
		},
		Description:  aws.String(config["LAMBDA_DESCRIPTION"]),
		FunctionName: aws.String(config["FUNCTION_NAME"]),
		Handler:      aws.String(config["HANDLER_NAME"]),
		MemorySize:   aws.Int64(memory),
		Publish:      aws.Bool(publishConfig),
		Role:         aws.String(config["DEV_ARN_IAM_ROLE"]),
		Runtime:      aws.String(lambda.RuntimeNodejs12X),
		Timeout:      aws.Int64(Timeout),
		VpcConfig: &lambda.VpcConfig{
			SecurityGroupIds: SecurityGroups,
			SubnetIds:        SubNetsID,
		},
	}
	return
}
