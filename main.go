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
		Content: "you are a very helpful assistant",
	},
	{
		Role:    openai.ChatMessageRoleUser,
		Content: "How can I plan my career development?",
	},
	{
		Role: openai.ChatMessageRoleAssistant,
		Content: `Planning your career development can be an important step towards achieving your professional goals. Here are some steps you can take to plan your career development:
1. Assess your current skills and strengths: Before you start planning your career development, it's important to have a good understanding of your current skills and strengths. This can help you identify areas where you need to improve and areas where you excel.
2. Identify your career goals: Think about what you want to achieve in your career. This can include short-term and long-term goals, such as learning a new skill, getting a promotion, or starting your own business.
2. Research career paths: Once you have identified your career goals, research different career paths that can help you achieve those goals. Look for job descriptions, career websites, and other resources to learn more about the skills and experience needed for different roles.`,
	},
}

func init() {
	bot := openwechat.DefaultBot()
	//bot := openwechat.DefaultBot(openwechat.Desktop) // deskop mode，you can switch deskop mode if defualt can not login

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
		fmt.Println(msg.Content)
		// simple match processing
		if strings.Contains(msg.Content, "生成图片") || strings.Contains(msg.Content, "generate image") {
			return replyImage(msg)
		}
		return replyText(msg)
	}
	return nil

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
	go addResultToMessage(result, msg)
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

// callOpenaiChat
func callOpenaiChat(messages []openai.ChatCompletionMessage) (string, error) {
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
