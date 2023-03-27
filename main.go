package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	openai "github.com/sashabaranov/go-openai"
	"github.com/silenceper/wechat/cache"
	"github.com/silenceper/wechat/v2"
	offConfig "github.com/silenceper/wechat/v2/officialaccount/config"
	"github.com/silenceper/wechat/v2/officialaccount/message"
)

var openaiToken, AppID, AppSecret, AppToken, EncodingAESKey string
var ok bool
var aiClient *openai.Client
var questions, answers map[string]string
var userMessage map[string][]openai.ChatCompletionMessage

func init() {
	openaiToken, ok = os.LookupEnv("OPENAI_TOKEN")
	if !ok {
		log.Fatal("Failed to read OPENAI_TOKEN environment variable.")
	}
	AppID, ok = os.LookupEnv("APP_ID")
	if !ok {
		log.Fatal("Failed to read APP_ID environment variable.")
	}
	AppSecret, ok = os.LookupEnv("APP_SECRET")
	if !ok {
		log.Fatal("Failed to read APP_SECRET environment variable.")
	}
	AppToken, ok = os.LookupEnv("APP_TOKEN")
	if !ok {
		log.Fatal("Failed to read APP_TOKEN environment variable.")
	}
	EncodingAESKey, ok = os.LookupEnv("ENCODING_AES_KEY")
	if !ok {
		log.Fatal("Failed to read ENCODING_AES_KEY environment variable.")
	}
	aiClient = openai.NewClient(openaiToken)
	answers = make(map[string]string)
	questions = make(map[string]string)
	userMessage = make(map[string][]openai.ChatCompletionMessage)
}

func Request(user, question string) string {
	ch := make(chan struct{})
	var ans, final string
	go func() {
		msgs := userMessage[user]
		msgs = append(msgs, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleUser,
			Content: question,
		})

		resp, err := aiClient.CreateChatCompletion(
			context.Background(),
			openai.ChatCompletionRequest{
				Model:     openai.GPT3Dot5Turbo,
				MaxTokens: 1000,
				User:      user,
				Messages:  msgs,
			},
		)
		if err != nil {
			ans = fmt.Sprintf("ChatCompletion error: %v\n", err)
		} else {
			ans = resp.Choices[0].Message.Content
			msgs = append(msgs, openai.ChatCompletionMessage{
				Role:    openai.ChatMessageRoleAssistant,
				Content: ans,
			})
		}
		if len(msgs) > 20 {
			msgs = msgs[2:]
		}
		userMessage[user] = msgs
		answers[question] = ans
		ch <- struct{}{}
	}()
	timer := time.NewTimer(4 * time.Second)
	select {
	case <-timer.C:
		final = ""
		go func() {
			<-ch
		}()
	case <-ch:
		final = answers[question]
	}
	if final == "" {
		final = "这个问题比较难，AI 正在思考，稍等几秒再次输入 “？”（中文符号）获取答案。"
	}
	return final
}

func Serve(w http.ResponseWriter, r *http.Request) {

	wc := wechat.NewWechat()
	memory := cache.NewMemory()
	cfg := &offConfig.Config{
		AppID:          AppID,
		AppSecret:      AppSecret,
		Token:          AppToken,
		EncodingAESKey: EncodingAESKey,
		Cache:          memory,
	}
	officialAccount := wc.GetOfficialAccount(cfg)
	// 传入request和responseWriter
	server := officialAccount.GetServer(r, w)
	// 设置接收消息的处理方法
	server.SetMessageHandler(func(msg *message.MixMessage) *message.Reply {
		if msg.Content == "？" {
			qs := questions[string(msg.FromUserName)]
			as := answers[qs]
			if as == "" {
				as = "AI 还在思考，再等几秒输入 “？”查询"
			} else {
				as = "上一次你说的是：\n" + qs + "\n\n我回答的是：\n" + as + "\n\n如果不是你刚问的，也可能是 AI 还没想出来，请稍等几秒继续输入“？”获取回答。"
			}
			return &message.Reply{MsgType: message.MsgTypeText, MsgData: message.NewText(as)}
		}
		questions[string(msg.FromUserName)] = msg.Content
		var text = message.NewText(Request(string(msg.FromUserName), msg.Content))
		return &message.Reply{MsgType: message.MsgTypeText, MsgData: text}
	})

	// 处理消息接收以及回复
	err := server.Serve()
	if err != nil {
		fmt.Println(err)
		return
	}
	// 发送回复的消息
	server.Send()
}

func main() {
	http.HandleFunc("/", Serve)
	log.Fatal(http.ListenAndServe(":80", nil))
}
