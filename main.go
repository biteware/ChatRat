package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"time"

	markov "github.com/IAmPattycakes/Go-Markov/v2"
	"github.com/gempir/go-twitch-irc/v2"
)

type ChatRat struct {
	graph           markov.Graph
	client          *twitch.Client
	trustedUsers    []string
	trustedUserFile string
	chatLog         string
	streamName      string
	oauth           string
	commandStarter  string
	botName         string
	ignoredUsers    []string
	ignoredUserFile string

	chatDelay    []string
	chatPaused   bool
	delayChanged bool
	chatTrigger  time.Timer
	lastGoodTime time.Duration //The last time that was properly parsed. This shouldn't have to be used, but if the error checking fails for some reason, well it'll keep things running.

	catKisses        []time.Time
	catKissTimeout   time.Duration
	catKissThreshold int
	catKissCooldown  time.Duration
	catKissLastTime  time.Time

	heCrazies        []time.Time
	heCrazyTimeout   time.Duration
	heCrazyThreshold int
	heCrazyCooldown  time.Duration
	heCrazyLastTime  time.Time
}

func main() {
	var rat ChatRat
	oauth := flag.String("oauth", "", "The oauth code for the twitch bot")
	streamName := flag.String("stream", "", "The name of the stream to join")
	botName := flag.String("botname", "", "The name of the bot")
	chatLog := flag.String("chatlog", "chat.log", "The name of the chat log to use. chat.log is used as the default.")
	trustFile := flag.String("trustfile", "trust.list", "The name of the list of trusted users")
	ignoreFile := flag.String("ignorefile", "block.list", "The name of the list of ignored users")
	commandStarter := flag.String("command", "!chatrat", "The word to get the bot's attention for commands")

	flag.Parse()
	rat.oauth = *oauth
	rat.streamName = *streamName
	rat.botName = *botName
	rat.chatLog = *chatLog
	rat.trustedUserFile = *trustFile
	rat.ignoredUserFile = *ignoreFile
	rat.commandStarter = *commandStarter
	rat.chatDelay = make([]string, 1)
	rat.chatDelay[0] = "2m"
	rat.chatPaused = false

	rat.lastGoodTime = 10 * time.Second

	rat.catKissTimeout = 10 * time.Second
	rat.catKissThreshold = 3
	rat.catKissCooldown = 1 * time.Minute

	rat.heCrazyTimeout = 10 * time.Second
	rat.heCrazyThreshold = 3
	rat.heCrazyCooldown = 1 * time.Minute

	client := twitch.NewClient(rat.botName, rat.oauth)
	rat.client = client
	rat.client.OnPrivateMessage(func(message twitch.PrivateMessage) {
		if message.User.Name != "chatrat_" {
			rat.messageParser(message)
		}
	})
	//Loading the chat history to give the model something to go off of at the start.
	rat.loadChatLog()
	//Setting up the stuff for special users
	loadUserList(rat.trustedUserFile, &rat.trustedUsers)
	loadUserList(rat.ignoredUserFile, &rat.ignoredUsers)
	fmt.Println(rat.trustedUsers)
	fmt.Println(rat.ignoredUsers)

	client.Join(rat.streamName)
	defer client.Disconnect()
	defer client.Depart(rat.streamName)
	rat.speak("Hi chat I'm back! =^.^=")
	go rat.speechHandler()
	err := client.Connect()

	if err != nil {
		panic(err)
	}
}

func loadUserList(filename string, list *[]string) {
	file, err := os.Open(filename)
	quitnow := false
	if err != nil {
		log.Print(err)
		quitnow = true
	}
	defer file.Close()
	if quitnow {
		return
	}
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		*list = append(*list, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}
}

func (rat *ChatRat) speak(message string) {
	// log.Println("saying" + message)
	rat.client.Say(rat.streamName, message)
}

func contains(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}

func (rat *ChatRat) catKissCleaner() {
	arr := make([]time.Time, 0)
	for _, v := range rat.catKisses {
		if v.Add(rat.catKissTimeout).After(time.Now()) {
			arr = append(arr, v)
		}
	}
	rat.catKisses = arr
}

func (rat *ChatRat) heCrazyCleaner() {
	arr := make([]time.Time, 0)
	for _, v := range rat.heCrazies {
		if v.Add(rat.heCrazyTimeout).After(time.Now()) {
			arr = append(arr, v)
		}
	}
	rat.heCrazies = arr
}

func (rat *ChatRat) messageParser(message twitch.PrivateMessage) {
	messageStrings := strings.Split(message.Message, " ")
	//CatKissies
	if contains(messageStrings, "catKiss") {
		rat.catKissCleaner()
		rat.catKisses = append(rat.catKisses, time.Now())
		if len(rat.catKisses) > rat.catKissThreshold {
			if rat.catKissLastTime.Add(rat.catKissCooldown).Before(time.Now()) {
				rat.speak("catKiss")
			}
		}
	}
	if contains(messageStrings, "heCrazy") {
		rat.heCrazyCleaner()
		rat.heCrazies = append(rat.heCrazies, time.Now())
		if len(rat.heCrazies) > rat.heCrazyThreshold {
			if rat.heCrazyLastTime.Add(rat.heCrazyCooldown).Before(time.Now()) {
				rat.speak("heCrazy")
			}
		}
	}
	if (len(messageStrings) > 0) && (messageStrings[0] == rat.commandStarter) { //Starting a chatrat command
		if len(messageStrings) > 1 {
			switch rat.isUserTrusted(message.User.Name) {
			case true:
				switch messageStrings[1] {
				case "set": //Setting ChatRat variables
					if len(messageStrings) > 2 {
						switch messageStrings[2] {
						case "delay": //Setting the delay between messages
							if len(messageStrings) > 4 {
								if s, err := strconv.ParseFloat(messageStrings[3], 32); err == nil {
									if s < 0 {
										rat.speak("@" + message.User.Name + " I don't understand how a delay can be negative.")
										return
									}
									var timeExtension string
									switch messageStrings[4] {
									case "seconds", "Seconds", "second", "Second":
										timeExtension = "s"
									case "minutes", "Minutes", "minute", "Minute":
										timeExtension = "m"
									case "hours", "Hours", "hour", "Hour":
										timeExtension = "h"
									default:
										rat.speak("@" + message.User.Name + "I don't understand what unit of time you're speaking about.")
									}
									_, err := time.ParseDuration(messageStrings[3] + timeExtension)
									if err == nil {
										rat.speak("Sorry, I don't know how to set the time yet. I have a bad problem with talking more than I should when the delay gets set.")
										// rat.chatDelay = make([]string, 1)
										// rat.chatDelay[0] = messageStrings[3] + timeExtension
										// rat.delayChanged = true
									} else {
										log.Println(err)
										rat.speak("@" + message.User.Name + " I don't know what went wrong here. Please screenshot what you said and send to the #chatrat channel on the discord.")
									}
								} else if err != nil {
									rat.speak("@" + message.User.Name + " I see you're trying to set the delay, but you gave me a weird number. ChatRat doesn't know math very well.")
								}
							} else {
								rat.speak("@" + message.User.Name + " I didn't hear any delay from you. I need a number and either hours, minutes, or seconds, like \"3 minutes\" or \"10 seconds\"")
							}
						}
					} else {
						rat.speak("@" + message.User.Name + " I couldn't understand you, I only saw you say \"" + rat.commandStarter + " set\" without anything else.")
						return
					}
				case "stop":
					if !rat.chatTrigger.Stop() { //Stop the timer, but don't let the speech handler know.
						<-rat.chatTrigger.C
					}
					rat.chatPaused = true
					rat.speak("Okay daddy I'll stop talking = >.< =")
				case "start":
					rat.chatPaused = false
					rat.speak("Thankies for taking the muzzle off! =^.^=")
				case "ignore":
					if len(messageStrings) > 2 {
						rat.speak("Sorry @" + messageStrings[2] + ", I can't talk to you anymore")
						rat.ignoredUsers = append(rat.ignoredUsers, messageStrings[2])
					} else {
						rat.speak("@" + message.User.Name + " I didn't see a user to ignore.")
					}
				case "unignore":
					if len(messageStrings) > 2 {
						array := make([]string, 0)
						for _, v := range rat.ignoredUsers {
							if strings.ToLower(messageStrings[2]) != v {
								array = append(array, v)
							}
						}
						rat.ignoredUsers = array
						fmt.Println(rat.ignoredUsers)
					}
				case "trust":
					if len(messageStrings) > 2 {
						rat.speak("Okay @" + messageStrings[2] + ", I'll let you tell me things to do")
						rat.trustedUsers = append(rat.ignoredUsers, messageStrings[2])
					} else {
						rat.speak("@" + message.User.Name + " I didn't see a user to trust.")
					}
				case "untrust":
					if len(messageStrings) > 2 {
						array := make([]string, 0)
						for _, v := range rat.ignoredUsers {
							if strings.ToLower(messageStrings[2]) != v {
								array = append(array, v)
							}
						}
						rat.ignoredUsers = array
						rat.speak("Sorry @" + messageStrings[2] + ", I can't listen to commands from you anymore")
						fmt.Println(rat.ignoredUsers)
					}
				case "speak":
					rat.speak(rat.graph.GenerateMarkovString())
				default:
					rat.speak("@" + message.User.Name + " I couldn't understand you, I only saw you say \"" + rat.commandStarter + "\" before I got confused.")
					return
				}
			case false:
				rat.speak("Hi I'm ChatRat, I only let trusted people tell me what to do, but I guess you can say my name if you like =^.^=")
			}

		}
	} else {
		rat.writeText(message.Message)
		rat.graph.LoadPhrase(message.Message)
	}
}

func (rat *ChatRat) isUserTrusted(username string) bool {
	for _, u := range rat.trustedUsers {
		if username == u {
			return true
		}
	}
	return false
}

func (rat *ChatRat) speechDelayPicker() time.Duration {
	switch len(rat.chatDelay) {
	case 1:
		t, err := time.ParseDuration(rat.chatDelay[0])
		if err == nil {
			rat.lastGoodTime = t
			return t
		} else {
			log.Println("Error parsing time: " + rat.chatDelay[0])
			return rat.lastGoodTime
		}
	case 2:
		t1, err := time.ParseDuration(rat.chatDelay[0])
		if err != nil {
			log.Println("Error parsing time: " + rat.chatDelay[0])
			return rat.lastGoodTime
		}
		t2, err := time.ParseDuration(rat.chatDelay[1])
		if err != nil {
			log.Println("Error parsing time: " + rat.chatDelay[1])
			return rat.lastGoodTime
		}
		if t1 > t2 {
			t1, t2 = t2, t1 //Swap the times to make the time randomization math work nicely without having to duplicate a bunch of crap.
		}
		return time.Duration(rand.Int63n(int64(t2-t1/time.Millisecond))) * time.Millisecond
	case 0:
		log.Println("I don't have a proper delay set up")
		log.Println(rat.chatDelay)
		return 5 * time.Minute
	}
	log.Print("The chatDelay array seems to have a bad amount of inputs, here it is")
	log.Println(rat.chatDelay)
	return 5 * time.Minute
}

func (rat *ChatRat) speechHandler() {
	done := true
	for {
		if !rat.chatPaused {
			if rat.delayChanged {
				log.Println("Speaking from the delayChanged part")
				rat.speak(rat.graph.GenerateMarkovString())
				if !rat.chatTrigger.Stop() {
					log.Println("Couldnt stop the timer")
					<-rat.chatTrigger.C
				}
				rat.delayChanged = false
				done = true
			}
			if done {
				done = false
				rat.chatTrigger = *time.AfterFunc(rat.speechDelayPicker(), func() {
					rat.speak(rat.graph.GenerateMarkovString())
					done = true
				})
			}
		}
	}
}

func (rat *ChatRat) writeText(text string) {
	f, err := os.OpenFile(rat.chatLog, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatal(err)
	}
	if _, err := f.Write([]byte(text + "\n")); err != nil {
		log.Fatal(err)
	}
	if err := f.Close(); err != nil {
		log.Fatal(err)
	}
}

func (rat *ChatRat) loadChatLog() {
	file, err := os.Open(rat.chatLog)
	quitnow := false
	if err != nil {
		log.Print(err)
		quitnow = true
	}
	defer file.Close()
	if quitnow {
		return
	}
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		rat.graph.LoadPhrase(scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}
}
