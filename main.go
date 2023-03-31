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
		Content: `你好，我很高兴能帮助你。职业发展是一个非常重要的话题，你可以从以下几个方面考虑：
		你的兴趣爱好和技能：你可以考虑你的兴趣爱好和技能，看看哪些职业与你的兴趣爱好和技能相匹配。这样可以让你在工作中感到更有成就感和满足感。
		行业前景：你可以考虑一些行业的前景，看看哪些行业在未来几年内会有更好的发展前景。这样可以让你在职业发展中更有保障。
		学历和培训：你可以考虑你的学历和培训，看看哪些职业需要更高的学历和培训。这样可以让你更好地规划你的职业发展。
		希望这些建议能帮到你。你还有其他问题吗？`,
	},
}

func init() {
	//bot := openwechat.DefaultBot()
	bot := openwechat.DefaultBot(openwechat.Desktop) // deskop mode，you can switch deskop mode if defualt can not login

	// Register message handler function
	bot.MessageHandler = func(msg *openwechat.Message) {
		replyChatMsg(msg)
	}

	// Register login QR code callback
	bot.UUIDCallback = openwechat.PrintlnQrcodeUrl

	// login
	if err := bot.Login(); err != nil {
		fmt.Println(err)
		return
	}

	client := openai.NewClient(os.Getenv("OPENAI_KEY")) // your openai key
	//client := openai.NewClient("")
	singleton = &OpenaiWechat{
		Openai:  client,
		ChatBot: bot,
	}
	singleton.userDialogue = make(map[string][]openai.ChatCompletionMessage)

	// lock goroutine, until an exception occurs or the user actively exits
	bot.Block()
}
func main() {
}

// replyChatMsg
func replyChatMsg(msg *openwechat.Message) error {

	if msg.IsSendByGroup() { // only accept group messages and send them to yourself
		if !msg.IsAt() {
			return nil
		}
	} else if !msg.IsSendByFriend() { // non-group messages only accept messages from your own friends
		return nil
	}

	if msg.IsSendBySelf() { // self sent to self no reply
		return nil
	}
	// only handle text messages
	if msg.IsText() {
		msg.Content = strings.Replace(msg.Content, "@GPT3.5 ", "", 1)
		fmt.Println(msg.Content)
		// simple match processing
		if isImage, _ := isImageContent(msg.Content); isImage {
			return replyImage(msg)
		}
		return replyText(msg)
	}
	return nil

}

// 图片生成校验
var imageMessage = []openai.ChatCompletionMessage{
	{
		Role:    openai.ChatMessageRoleSystem,
		Content: "你现在是一个语义识别助手，用户输入一个文本，你根据文本的内容来判断用户是不是想生成图片，是的话你就回复是，不是的话你就回复否，记住只能回复：是 或者 否",
	},
	{
		Role:    openai.ChatMessageRoleUser,
		Content: "我想生成一张小花猫的图片",
	},
	{
		Role:    openai.ChatMessageRoleAssistant,
		Content: "是",
	},
	{
		Role:    openai.ChatMessageRoleUser,
		Content: "请问冬天下雨我该穿什么衣服",
	},
	{
		Role:    openai.ChatMessageRoleAssistant,
		Content: "否",
	},
}

// isImageConntent
func isImageContent(content string) (bool, error) {
	temp := append(imageMessage, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleUser,
		Content: content,
	})
	result, err := callOpenaiChat(temp)
	if err != nil {
		fmt.Printf("isImageConntent callOpenaiChat error: %v\n", err)
		return false, err
	}
	fmt.Printf("isImageContent result: %s", result)
	if strings.TrimSpace(result) == "是" {
		return true, nil
	}
	return false, nil

}

// replyImage
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

// replyText
func replyText(msg *openwechat.Message) error {
	messages, err := genMessage(msg)
	if err != nil {
		return err
	}
	result, err := callOpenaiChat(messages)
	if err != nil {
		fmt.Println(err)
		return err
	}
	msg.ReplyText(result)
	addResultToMessage(result, msg)
	return nil
}

// dialogue context max length
const maxLength = 33

// temporary folder to save pictures
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
		Size:           openai.CreateImageSize512x512,
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

// callOpenaiChat
func callOpenaiChat(messages []openai.ChatCompletionMessage) (string, error) {
	resp, err := singleton.Openai.CreateChatCompletion(
		context.Background(),
		openai.ChatCompletionRequest{
			Model:     openai.GPT3Dot5Turbo,
			Messages:  messages,
			MaxTokens: 512,
		},
	)
	if err != nil {
		fmt.Printf("ChatCompletion error: %v\n", err)
		return "", err
	}
	fmt.Println(resp.Choices[0].Message.Content)
	return resp.Choices[0].Message.Content, nil
}
