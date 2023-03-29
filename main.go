package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image/png"
	"os"
	"strings"
	"time"

	"github.com/eatmoreapple/openwechat"
	openai "github.com/sashabaranov/go-openai"
)

// OpenaiWechat
type OpenaiWechat struct {
	ChatBot      *openwechat.Bot                           // chat bot
	Openai       *openai.Client                            // openai client
	userDialogue map[string][]openai.ChatCompletionMessage // save user gialogue context
}

var singleton *OpenaiWechat

// firstUserDialogue
var firstUserDialogue = []openai.ChatCompletionMessage{
	{
		Role:    openai.ChatMessageRoleSystem,
		Content: "你是一个非常有帮助的助手",
	},
	{
		Role:    openai.ChatMessageRoleUser,
		Content: "请问我怎么规划我的职业发展呢？",
	},
	{
		Role: openai.ChatMessageRoleAssistant,
		Content: `职业发展是一个非常重要的话题，你可以从以下几个方面考虑：
		你的兴趣爱好和技能：你可以考虑你的兴趣爱好和技能，看看哪些职业与你的兴趣爱好和技能相匹配。这样可以让你在工作中感到更有成就感和满足感。
		行业前景：你可以考虑一些行业的前景，看看哪些行业在未来几年内会有更好的发展前景。这样可以让你在职业发展中更有保障。
		学历和培训：你可以考虑你的学历和培训，看看哪些职业需要更高的学历和培训。这样可以让你更好地规划你的职业发展。`,
	},
}

func init() {
	bot := openwechat.DefaultBot()
	//bot := openwechat.DefaultBot(openwechat.Desktop) // 桌面模式，上面登录不上的可以尝试切换这种模式

	// 注册消息处理函数
	bot.MessageHandler = func(msg *openwechat.Message) {
		replyChatMsg(msg)
	}

	// 注册登陆二维码回调
	bot.UUIDCallback = openwechat.PrintlnQrcodeUrl

	// 登陆
	if err := bot.Login(); err != nil {
		fmt.Println(err)
		return
	}

	client := openai.NewClient(os.Getenv("OPENAI_KEY"))
	//client := openai.NewClient("")
	singleton = &OpenaiWechat{
		Openai:  client,
		ChatBot: bot,
	}
	singleton.userDialogue = make(map[string][]openai.ChatCompletionMessage)

	// 阻塞主goroutine, 直到发生异常或者用户主动退出
	bot.Block()
}
func main() {
}

// replyChatMsg
func replyChatMsg(msg *openwechat.Message) error {

	if msg.IsSendByGroup() { // 只接受群消息且发送给自己的
		if !msg.IsAt() {
			return nil
		}
	} else if !msg.IsSendByFriend() { // 非群消息只接受自己好友的消息
		return nil
	}

	if msg.IsSendBySelf() { // 自己发送给自己的不回复
		return nil
	}
	// 文本消息
	if msg.IsText() {
		fmt.Println(msg.Content)
		if strings.Contains(msg.Content, "生成图片") {
			return replyImage(msg)
		}
		return replyText(msg)
	}
	return nil

}

// 回复图片
func replyImage(msg *openwechat.Message) error {
	path, err := generateImage(msg)
	if err != nil {
		fmt.Printf("replyImage generateImage error: %v\n", err)
		return err
	}
	img, err := os.Open(path)
	if err != nil {
		fmt.Printf("replyImage Open error: %v\n", err)
		return err
	}
	defer img.Close()
	msg.ReplyImage(img)
	return nil
}

// 处理文本消息
func replyText(msg *openwechat.Message) error {
	messages, err := genMessage(msg)
	if err != nil {
		return err
	}
	result, err := callOpenaiApi(messages)
	if err != nil {
		fmt.Println(err)
		return err
	}
	msg.ReplyText(result)
	go addResultToMessage(result, msg)
	return nil
}

// maxLength
const maxLength = 33
const filePath = "./images/"

// genMessage
func genMessage(msg *openwechat.Message) ([]openai.ChatCompletionMessage, error) {
	// TODO
	if _, ok := singleton.userDialogue[msg.FromUserName]; !ok {
		singleton.userDialogue[msg.FromUserName] = firstUserDialogue
	} else if len(singleton.userDialogue[msg.FromUserName]) >= maxLength {
		singleton.userDialogue[msg.FromUserName] = singleton.userDialogue[msg.FromUserName][2:]
	}
	element := openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleUser,
		Content: msg.Content,
	}
	singleton.userDialogue[msg.FromUserName] = append(singleton.userDialogue[msg.FromUserName], element)
	return singleton.userDialogue[msg.FromUserName], nil
}

// addResultToMessage
func addResultToMessage(result string, msg *openwechat.Message) error {
	element := openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleAssistant,
		Content: result,
	}
	singleton.userDialogue[msg.FromUserName] = append(singleton.userDialogue[msg.FromUserName], element)
	return nil
}

// generateImage
func generateImage(msg *openwechat.Message) (string, error) {
	ctx := context.Background()

	// Sample image by link
	reqUrl := openai.ImageRequest{
		Prompt:         msg.Content,
		Size:           openai.CreateImageSize256x256,
		ResponseFormat: openai.CreateImageResponseFormatB64JSON,
		N:              1,
	}

	respBase64, err := singleton.Openai.CreateImage(ctx, reqUrl)
	if err != nil {
		fmt.Printf("Image creation error: %v\n", err)
		return "", err
	}
	imgBytes, err := base64.StdEncoding.DecodeString(respBase64.Data[0].B64JSON)
	if err != nil {
		fmt.Printf("Base64 decode error: %v\n", err)
		return "", err
	}

	r := bytes.NewReader(imgBytes)
	imgData, err := png.Decode(r)
	if err != nil {
		fmt.Printf("PNG decode error: %v\n", err)
		return "", err
	}
	filePath := fmt.Sprintf("%s%d.png", filePath, time.Now().UnixMicro())
	file, err := os.Create(filePath)
	if err != nil {
		fmt.Printf("File creation error: %v\n", err)
		return "", err
	}
	defer file.Close()

	if err := png.Encode(file, imgData); err != nil {
		fmt.Printf("PNG encode error: %v\n", err)
		return "", err
	}
	return filePath, nil
}

// generateMessage
func callOpenaiApi(messages []openai.ChatCompletionMessage) (string, error) {
	resp, err := singleton.Openai.CreateChatCompletion(
		context.Background(),
		openai.ChatCompletionRequest{
			Model:    openai.GPT3Dot5Turbo,
			Messages: messages,
		},
	)
	if err != nil {
		fmt.Printf("ChatCompletion error: %v\n", err)
		return "", err
	}
	fmt.Println(resp.Choices[0].Message.Content)
	return resp.Choices[0].Message.Content, nil
}
