package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/digital-idea/dilog"
	"github.com/disintegration/imaging"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
)

// handleAPIAddReviewStatusMode 함수는 review status mode에 review를 추가하는 핸들러이다.
func handleAPIAddReviewStatusMode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Post Only", http.StatusMethodNotAllowed)
		return
	}
	type Recipe struct {
		UserID string `json:"userid"`
		Review
	}
	rcp := Recipe{}
	session, err := mgo.Dial(*flagDBIP)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer session.Close()
	rcp.UserID, _, err = TokenHandler(r, session)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	r.ParseForm()
	project := r.FormValue("project")
	if project == "" {
		http.Error(w, "project를 설정해주세요", http.StatusBadRequest)
		return
	}
	rcp.Review.Project = project

	name := r.FormValue("name")
	if name == "" {
		http.Error(w, "name을 설정해주세요", http.StatusBadRequest)
		return
	}
	rcp.Review.Name = strings.TrimSpace(name) // 앞뒤로 사용자가 빈문자열을 넣을 수 있다. 제거한다.

	task := r.FormValue("task")
	if task == "" {
		http.Error(w, "task를 설정해주세요", http.StatusBadRequest)
		return
	}
	rcp.Review.Task = task
	typ := r.FormValue("type")
	if typ == "" {
		rcp.Review.Type = "clip"

	}
	rcp.Review.Type = typ
	ext := r.FormValue("ext")
	if ext == "" {
		rcp.Review.Ext = ".mp4"
	}
	rcp.Review.Ext = ext
	// 리뷰 등록시 현재 Task status를 review itemStatus로 설정한다.
	_, taskinfo, err := GetTask(session, rcp.Review.Project, rcp.Review.Name, rcp.Review.Task)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// project, name, task를 이용해서 current status를 가지고 온다.
	rcp.Review.ItemStatus = taskinfo.StatusV2
	author := r.FormValue("author")
	if author == "" {
		rcp.Review.Author = rcp.UserID
	}
	rcp.Review.Author = author
	rcp.Review.AuthorNameKor = r.FormValue("authornamekor")
	if rcp.Review.AuthorNameKor == "" {
		// authornamekor 값이 비어있다면, 사용자의 아이디를 이용해서 DB에 등록된 이름을 가지고 온다.
		user, err := getUser(session, author)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		rcp.Review.AuthorNameKor = user.LastNameKor + user.FirstNameKor
	}
	path := r.FormValue("path")
	if path == "" {
		http.Error(w, "path를 설정해주세요", http.StatusBadRequest)
		return
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		http.Error(w, path+"파일이 서버에 존재하지 않습니다", http.StatusBadRequest)
		return
	}
	rcp.Review.Path = path
	fpsString := r.FormValue("fps")
	fps, err := strconv.ParseFloat(fpsString, 64)
	if err != nil {
		http.Error(w, fmt.Sprintf("%s 는 fps로 사용할 수 없는 값 입니다", fpsString), http.StatusBadRequest)
		return
	}
	rcp.Review.Fps = fps
	rcp.Review.Description = r.FormValue("description")
	rcp.Review.CameraInfo = r.FormValue("camerainfo")
	progress := r.FormValue("progress")
	if progress != "" {
		n, err := strconv.Atoi(progress)
		if err != nil {
			http.Error(w, "progress의 값이 숫자가 아닙니다.", http.StatusBadRequest)
			return
		}
		if !(0 < n && n < 101) {
			http.Error(w, "progress의 값은 1~100 사이의 수가 되어야 합니다.", http.StatusBadRequest)
			return
		}
		rcp.Review.Progress = n
	}
	rcp.Review.Createtime = time.Now().Format(time.RFC3339)
	rcp.Review.Updatetime = rcp.Review.Createtime
	mainVer, err := strconv.Atoi(r.FormValue("mainversion"))
	if err != nil {
		http.Error(w, "mainversion은 숫자로 입력되어 합니다", http.StatusBadRequest)
		return
	}
	rcp.MainVersion = mainVer
	subVer, err := strconv.Atoi(r.FormValue("subversion"))
	if err != nil {
		rcp.SubVersion = 0 // 서브버전은 없을 수 있다. 설정되지 않는다면 0값을 기본으로 한다.
	} else {
		rcp.SubVersion = subVer
	}
	rcp.Review.ID = bson.NewObjectId()
	rcp.Review.ProcessStatus = "wait" // ffmpeg 연산을 기다리는 상태로 등록한다.

	rcp.Review.RemoveAfterProcess = str2bool(r.FormValue("removeafterprocess"))
	rcp.Review.OutputDataPath = r.FormValue("outputdatapath")

	err = addReview(session, rcp.Review)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// log
	err = dilog.Add(*flagDBIP, host, fmt.Sprintf("AddReview: %s, %s, %s", rcp.Review.Name, rcp.Review.Task, rcp.Review.Path), rcp.Review.Project, rcp.Review.Name, "OpenPipelineIO", rcp.UserID, 180)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// slack log
	err = slacklog(session, rcp.Project, fmt.Sprintf("AddReview: %s, %s\nProject: %s, Name: %s, Author: %s", rcp.Review.Task, rcp.Review.Path, rcp.Review.Project, rcp.Review.Name, rcp.UserID))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data, err := json.Marshal(rcp.Review)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// handleAPISearchReview 함수는 review를 검색하는 핸들러이다.
func handleAPISearchReview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Post Only", http.StatusMethodNotAllowed)
		return
	}
	type Recipe struct {
		UserID  string   `json:"userid"`
		Reviews []Review `json:"review"`
	}
	rcp := Recipe{}
	session, err := mgo.Dial(*flagDBIP)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer session.Close()
	rcp.UserID, _, err = TokenHandler(r, session)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	r.ParseForm()
	searchword := r.FormValue("searchword")
	if searchword == "" {
		http.Error(w, "searchword를 설정해주세요", http.StatusBadRequest)
		return
	}
	reviews, err := searchReview(session, searchword)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	rcp.Reviews = reviews
	data, err := json.Marshal(rcp.Reviews)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// handleAPIReview 함수는 id를 받아서 review 데이터를 반환하는 핸들러이다.
func handleAPIReview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Post Only", http.StatusMethodNotAllowed)
		return
	}
	session, err := mgo.Dial(*flagDBIP)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer session.Close()
	r.ParseForm()
	id := r.FormValue("id")
	if id == "" {
		http.Error(w, "id를 설정해주세요", http.StatusBadRequest)
		return
	}
	review, err := getReview(session, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data, err := json.Marshal(review)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// handleAPISetReviewItemStatus 함수는 review의 itemstatus를 설정하는 RestAPI 이다.
func handleAPISetReviewItemStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Post Only", http.StatusMethodNotAllowed)
		return
	}
	type Recipe struct {
		UserID     string `json:"userid"`
		ID         string `json:"id"`
		ItemStatus string `json:"itemstatus"`
	}
	rcp := Recipe{}
	session, err := mgo.Dial(*flagDBIP)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer session.Close()
	rcp.UserID, _, err = TokenHandler(r, session)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	r.ParseForm()
	reviewID := r.FormValue("id")
	if reviewID == "" {
		http.Error(w, "id를 설정해주세요", http.StatusBadRequest)
		return
	}
	rcp.ID = reviewID

	itemStatus := r.FormValue("itemstatus")
	if itemStatus == "" {
		http.Error(w, "itemstatus를 설정해주세요", http.StatusBadRequest)
		return
	}
	rcp.ItemStatus = itemStatus
	// Review 데이터를 가지고 온다. 해당정보를 이용해서 Task 정보를 수정할 수 있는 정보를 수집한다.
	review, err := getReview(session, rcp.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 리뷰 ItemStatus를 설정한다.
	err = setReviewItemStatus(session, rcp.ID, itemStatus)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Type을 구한다.
	typ, err := Type(session, review.Project, review.Name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// 실제 아이템의 Shot, Asset Status를 설정한다.
	_, err = SetTaskStatusV2(session, review.Project, review.Name+"_"+typ, review.Task, itemStatus)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// log
	err = dilog.Add(*flagDBIP, host, fmt.Sprintf("Set Review Item Status: %s, %s", rcp.ID, rcp.ItemStatus), review.Project, review.Name, "OpenPipelineIO", rcp.UserID, 180)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// slack log
	err = slacklog(session, review.Project, fmt.Sprintf("Set Review Item Status: %s, \nProject: %s, Name: %s, Author: %s", itemStatus, review.Project, review.Name, rcp.UserID))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data, err := json.Marshal(rcp)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// handleAPIAddReviewComment 함수는 review에 comment를 설정하는 RestAPI 이다.
func handleAPIAddReviewComment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Post Only", http.StatusMethodNotAllowed)
		return
	}
	type Recipe struct {
		UserID               string `json:"userid"`
		ID                   string `json:"id"`
		Text                 string `json:"text"`
		Media                string `json:"media"`
		MediaTitle           string `json:"mediatitle"`
		Author               string `json:"author"`
		AuthorName           string `json:"authorname"`
		Date                 string `json:"date"`
		Frame                int    `json:"frame"`
		FrameComment         bool   `json:"framecomment"`
		ProductionStartFrame int    `json:"productionstartframe"` // UX 를 그릴 때 필요하다.
		Protocol             string `json:"protocol"`
	}
	rcp := Recipe{}
	rcp.Protocol = CachedAdminSetting.Protocol
	rcp.ProductionStartFrame = CachedAdminSetting.ProductionStartFrame
	session, err := mgo.Dial(*flagDBIP)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer session.Close()
	rcp.UserID, _, err = TokenHandler(r, session)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	// 사용자의 이름을 구한다.
	u, err := getUser(session, rcp.UserID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized) // 사용자가 존재하지 않으면 당연히 Comment를 작성하면 안된다.
		return
	}
	rcp.AuthorName = u.LastNameKor + u.FirstNameKor
	r.ParseForm()
	id := r.FormValue("id")
	if id == "" {
		http.Error(w, "id를 설정해주세요", http.StatusBadRequest)
		return
	}
	rcp.ID = id
	rcp.Text = r.FormValue("text")
	rcp.Media = r.FormValue("media")
	if rcp.Text == "" && rcp.Media == "" {
		http.Error(w, "comment(text) 또는 첨부파일(media) 값 둘중 하나는 반드시 입력되어야 합니다", http.StatusBadRequest)
		return
	}
	rcp.MediaTitle = r.FormValue("mediatitle")

	review, err := getReview(session, rcp.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	rcp.FrameComment = str2bool(r.FormValue("framecomment"))
	frame, err := strconv.Atoi(r.FormValue("frame"))
	if err != nil {
		frame = 0
	}
	if rcp.FrameComment {
		rcp.Frame = frame
	}
	cmt := Comment{}
	cmt.Date = time.Now().Format(time.RFC3339)
	rcp.Date = cmt.Date
	cmt.Author = rcp.UserID
	rcp.Author = rcp.UserID
	cmt.AuthorName = rcp.AuthorName
	cmt.Text = rcp.Text
	cmt.Media = rcp.Media
	cmt.MediaTitle = rcp.MediaTitle
	cmt.Frame = rcp.Frame

	err = addReviewComment(session, rcp.ID, cmt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// log
	err = dilog.Add(*flagDBIP, host, fmt.Sprintf("Add Review Comment: %s, %s", rcp.ID, rcp.Text), review.Project, review.Name, "OpenPipelineIO", rcp.UserID, 180)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// slack log
	err = slacklog(session, review.Project, fmt.Sprintf("Add Review Comment: %s, \nProject: %s, Name: %s, Author: %s", rcp.Text, review.Project, review.Name, rcp.UserID))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// rocketchat
	if CachedAdminSetting.EnableRocketChat {
		// 해당 샷의 정보를 가지고 온다.
		typ, err := Type(session, review.Project, review.Name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		item, err := getItem(session, review.Project, review.Name+"_"+typ)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// 아티스트를 추출한다.
		var rocketChatIDs []string
		for _, i := range item.Tasks {
			user, err := getUser(session, i.UserID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if user.RocketChatID == "" {
				continue
			}
			rocketChatIDs = append(rocketChatIDs, user.RocketChatID)
		}
		// 해당 아티스트의 로켓쳇 아이디마다 메시지를 전송한다.
		for _, rocketChatID := range rocketChatIDs {
			msg := HookMessage{}
			msg.Channel = "@" + rocketChatID
			title := fmt.Sprintf("[Review] %s %s_%s_v%s", review.Project, review.Name, review.Task, ProductionVersionFormat(review.MainVersion))
			if review.SubVersion != 0 {
				title += fmt.Sprintf("_w%s", ProductionVersionFormat(review.SubVersion))
			}
			msg.Text = title + " 리뷰데이터에 코멘트가 작성되었습니다.: " + rcp.Text
			resp, err := msg.SendRocketChat()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if !resp.Success {
				http.Error(w, "not success rocketchat message", http.StatusInternalServerError)
				return
			}
		}
	}

	data, err := json.Marshal(rcp)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// handleAPIAddReviewStatusModeComment 함수는 review status mode에서 comment를 설정하는 RestAPI 이다.
func handleAPIAddReviewStatusModeComment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Post Only", http.StatusMethodNotAllowed)
		return
	}

	type Recipe struct {
		UserID               string `json:"userid"`
		ID                   string `json:"id"`
		Text                 string `json:"text"`
		Media                string `json:"media"`
		MediaTitle           string `json:"mediatitle"`
		Author               string `json:"author"`
		AuthorName           string `json:"authorname"`
		Date                 string `json:"date"`
		ItemStatus           string `json:"itemstatus"`
		Frame                int    `json:"frame"`
		FrameComment         bool   `json:"framecomment"`
		ProductionStartFrame int    `json:"productionstartframe"` // UX 를 그릴 때 필요하다.
		Protocol             string `json:"protocol"`
	}
	rcp := Recipe{}
	rcp.Protocol = CachedAdminSetting.Protocol
	rcp.ProductionStartFrame = CachedAdminSetting.ProductionStartFrame
	session, err := mgo.Dial(*flagDBIP)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer session.Close()
	rcp.UserID, _, err = TokenHandler(r, session)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	// 사용자의 이름을 구한다.
	u, err := getUser(session, rcp.UserID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized) // 사용자가 존재하지 않으면 당연히 Comment를 작성하면 안된다.
		return
	}
	rcp.AuthorName = u.LastNameKor + u.FirstNameKor
	r.ParseForm()
	id := r.FormValue("id")
	if id == "" {
		http.Error(w, "id를 설정해주세요", http.StatusBadRequest)
		return
	}
	rcp.ID = id
	rcp.Text = r.FormValue("text")
	rcp.Media = r.FormValue("media")
	if rcp.Text == "" && rcp.Media == "" {
		http.Error(w, "comment(text) 또는 첨부파일(media) 값 둘중 하나는 반드시 입력되어야 합니다", http.StatusBadRequest)
		return
	}
	rcp.MediaTitle = r.FormValue("mediatitle")

	review, err := getReview(session, rcp.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	rcp.ItemStatus = r.FormValue("itemstatus")
	// Status가 존재하는지 체크하기
	_, err = GetStatus(session, rcp.ItemStatus)
	if err != nil {
		if err == mgo.ErrNotFound {
			http.Error(w, rcp.ItemStatus+" Status는 존재하지 않는 Status 입니다", http.StatusBadRequest)
			return
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

	}

	rcp.FrameComment = str2bool(r.FormValue("framecomment"))
	frame, err := strconv.Atoi(r.FormValue("frame"))
	if err != nil {
		frame = 0
	}
	if rcp.FrameComment {
		rcp.Frame = frame
	}
	cmt := Comment{}
	cmt.Date = time.Now().Format(time.RFC3339)
	rcp.Date = cmt.Date
	cmt.Author = rcp.UserID
	rcp.Author = rcp.UserID
	cmt.AuthorName = rcp.AuthorName
	cmt.Text = rcp.Text
	cmt.Media = rcp.Media
	cmt.MediaTitle = rcp.MediaTitle
	cmt.ItemStatus = rcp.ItemStatus
	cmt.Frame = rcp.Frame

	err = addReviewComment(session, rcp.ID, cmt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// log
	err = dilog.Add(*flagDBIP, host, fmt.Sprintf("Add Review Comment: %s, %s", rcp.ID, rcp.Text), review.Project, review.Name, "OpenPipelineIO", rcp.UserID, 180)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// slack log
	err = slacklog(session, review.Project, fmt.Sprintf("Add Review Comment: %s, \nProject: %s, Name: %s, Author: %s", rcp.Text, review.Project, review.Name, rcp.UserID))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// rocketchat
	if CachedAdminSetting.EnableRocketChat {
		// 해당 샷의 정보를 가지고 온다.
		typ, err := Type(session, review.Project, review.Name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		item, err := getItem(session, review.Project, review.Name+"_"+typ)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// 아티스트를 추출한다.
		var rocketChatIDs []string
		for _, i := range item.Tasks {
			user, err := getUser(session, i.UserID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			rocketChatIDs = append(rocketChatIDs, user.RocketChatID)
		}
		// 해당 아티스트의 로켓쳇 아이디마다 메시지를 전송한다.
		for _, rocketChatID := range rocketChatIDs {
			msg := HookMessage{}
			msg.Channel = "@" + rocketChatID
			title := fmt.Sprintf("[%s Status] %s %s_%s_v%02d", rcp.ItemStatus, review.Project, review.Name, review.Task, review.MainVersion)
			if review.SubVersion != 0 {
				title += fmt.Sprintf("_w%02d", review.SubVersion)
			}
			msg.Text = title + " 리뷰데이터에 코멘트가 작성되었습니다.: " + rcp.Text
			resp, err := msg.SendRocketChat()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if !resp.Success {
				http.Error(w, "not success rocketchat message", http.StatusInternalServerError)
				return
			}
		}
	}

	data, err := json.Marshal(rcp)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// handleAPIEditReviewComment 함수는 리뷰에서 코멘트를 수정합니다.
func handleAPIEditReviewComment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Post Only", http.StatusMethodNotAllowed)
		return
	}
	type Recipe struct {
		ID                   string `json:"id"`
		Time                 string `json:"time"`
		Text                 string `json:"text"`
		Media                string `json:"media"`
		UserID               string `json:"userid"`
		Frame                int    `json:"frame"`
		ProductionStartFrame int    `json:"productionstartframe"`
		Protocol             string `json:"protocol"`
	}
	rcp := Recipe{}
	rcp.Protocol = CachedAdminSetting.Protocol
	rcp.ProductionStartFrame = CachedAdminSetting.ProductionStartFrame
	session, err := mgo.Dial(*flagDBIP)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer session.Close()
	rcp.UserID, _, err = TokenHandler(r, session)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	r.ParseForm() // 받은 문자를 파싱합니다. 파싱되면 map이 됩니다.
	reviewID := r.FormValue("id")
	if reviewID == "" {
		http.Error(w, "id를 설정해주세요", http.StatusBadRequest)
		return
	}
	rcp.ID = reviewID
	reviewTime := r.FormValue("time")
	if reviewTime == "" {
		http.Error(w, "time을 설정해주세요", http.StatusBadRequest)
		return
	}
	rcp.Time = reviewTime
	reviewText := r.FormValue("text")
	if reviewText == "" {
		http.Error(w, "text를 설정해주세요", http.StatusBadRequest)
		return
	}
	rcp.Text = reviewText
	rcp.Media = r.FormValue("media")
	frame, err := strconv.Atoi(r.FormValue("frame"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	rcp.Frame = frame
	err = EditReviewComment(session, rcp.ID, rcp.Time, rcp.Text, rcp.Media, rcp.Frame)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// json 으로 결과 전송
	data, err := json.Marshal(rcp)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// handleAPIRmReviewComment 함수는 리뷰에서 수정사항을 삭제합니다.
func handleAPIRmReviewComment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Post Only", http.StatusMethodNotAllowed)
		return
	}
	type Recipe struct {
		ID      string `json:"id"`
		Time    string `json:"time"`
		Project string `json:"project"`
		Name    string `json:"name"`
		Text    string `json:"text"`
		UserID  string `json:"userid"`
	}
	rcp := Recipe{}
	session, err := mgo.Dial(*flagDBIP)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer session.Close()
	rcp.UserID, _, err = TokenHandler(r, session)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	r.ParseForm() // 받은 문자를 파싱합니다. 파싱되면 map이 됩니다.
	reviewID := r.FormValue("id")
	if reviewID == "" {
		http.Error(w, "id를 설정해주세요", http.StatusBadRequest)
		return
	}
	rcp.ID = reviewID
	reviewTime := r.FormValue("time")
	if reviewTime == "" {
		http.Error(w, "time을 설정해주세요", http.StatusBadRequest)
		return
	}
	rcp.Time = reviewTime

	// ID를 이용해서 삭제할 리뷰아이템을 가져와 Project, Name, Text를 반환될 json에 설정합니다.
	review, err := getReview(session, rcp.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	rcp.Project = review.Project
	rcp.Name = review.Name
	for _, t := range review.Comments {
		if t.Date == reviewTime {
			rcp.Text = t.Text
			break
		}
	}

	// 리뷰데이터를 삭제합니다.
	err = RmReviewComment(session, rcp.ID, rcp.Time)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// log
	err = dilog.Add(*flagDBIP, host, fmt.Sprintf("Rm Review Comment: %s, %s", rcp.ID, rcp.Text), rcp.Project, rcp.Name, "OpenPipelineIO", rcp.UserID, 180)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// slack log
	err = slacklog(session, rcp.Project, fmt.Sprintf("Rm Review Comment: %s\nProject: %s, Name: %s, Author: %s", rcp.Text, rcp.Project, rcp.Name, rcp.UserID))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// json 으로 결과 전송
	data, err := json.Marshal(rcp)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// handleAPIRmReview 함수는 리뷰를 삭제합니다.
func handleAPIRmReview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Post Only", http.StatusMethodNotAllowed)
		return
	}
	type Recipe struct {
		ID     string `json:"id"`
		UserID string `json:"userid"`
	}
	rcp := Recipe{}
	session, err := mgo.Dial(*flagDBIP)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer session.Close()
	rcp.UserID, _, err = TokenHandler(r, session)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	r.ParseForm() // 받은 문자를 파싱합니다. 파싱되면 map이 됩니다.
	reviewID := r.FormValue("id")
	if reviewID == "" {
		http.Error(w, "id를 설정해주세요", http.StatusBadRequest)
		return
	}
	rcp.ID = reviewID
	review, err := getReview(session, reviewID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// 데이터 삭제
	reviewfile := fmt.Sprintf("%s/%s%s", CachedAdminSetting.ReviewDataPath, review.Ext, rcp.ID)
	if _, err := os.Stat(reviewfile); err == nil {
		err = os.Remove(reviewfile)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	// log
	err = dilog.Add(*flagDBIP, host, fmt.Sprintf("Rm Review: %s", rcp.ID), review.Project, review.Name, "OpenPipelineIO", rcp.UserID, 180)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// slack log
	err = slacklog(session, review.Project, fmt.Sprintf("Rm Review: %s, \nProject: %s, Name: %s, Author: %s", rcp.ID, review.Project, review.Name, rcp.UserID))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// DB삭제
	err = RmReview(session, rcp.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// json 으로 결과 전송
	data, err := json.Marshal(rcp)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// handleAPISetReviewProject 함수는 리뷰에서 Project를 설정합니다.
func handleAPISetReviewProject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Post Only", http.StatusMethodNotAllowed)
		return
	}
	type Recipe struct {
		ID      string `json:"id"`
		Project string `json:"project"`
		UserID  string `json:"userid"`
	}
	rcp := Recipe{}
	session, err := mgo.Dial(*flagDBIP)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer session.Close()
	rcp.UserID, _, err = TokenHandler(r, session)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	r.ParseForm() // 받은 문자를 파싱합니다. 파싱되면 map이 됩니다.
	reviewID := r.FormValue("id")
	if reviewID == "" {
		http.Error(w, "id를 설정해주세요", http.StatusBadRequest)
		return
	}
	rcp.ID = reviewID
	project := r.FormValue("project")
	if project == "" {
		http.Error(w, "project 를 설정해주세요", http.StatusBadRequest)
		return
	}
	rcp.Project = project
	err = SetReviewProject(session, rcp.ID, rcp.Project)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// json 으로 결과 전송
	data, err := json.Marshal(rcp)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// handleAPISetReviewTask 함수는 리뷰에서 Task를 설정합니다.
func handleAPISetReviewTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Post Only", http.StatusMethodNotAllowed)
		return
	}
	type Recipe struct {
		ID     string `json:"id"`
		Task   string `json:"task"`
		UserID string `json:"userid"`
	}
	rcp := Recipe{}
	session, err := mgo.Dial(*flagDBIP)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer session.Close()
	rcp.UserID, _, err = TokenHandler(r, session)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	r.ParseForm() // 받은 문자를 파싱합니다. 파싱되면 map이 됩니다.
	reviewID := r.FormValue("id")
	if reviewID == "" {
		http.Error(w, "id를 설정해주세요", http.StatusBadRequest)
		return
	}
	rcp.ID = reviewID
	task := r.FormValue("task")
	if task == "" {
		http.Error(w, "task 를 설정해주세요", http.StatusBadRequest)
		return
	}
	rcp.Task = task
	err = SetReviewTask(session, rcp.ID, rcp.Task)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// json 으로 결과 전송
	data, err := json.Marshal(rcp)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// handleAPISetReviewName 함수는 리뷰에서 Name을 설정합니다.
func handleAPISetReviewName(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Post Only", http.StatusMethodNotAllowed)
		return
	}
	type Recipe struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		UserID string `json:"userid"`
	}
	rcp := Recipe{}
	session, err := mgo.Dial(*flagDBIP)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer session.Close()
	rcp.UserID, _, err = TokenHandler(r, session)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	r.ParseForm() // 받은 문자를 파싱합니다. 파싱되면 map이 됩니다.
	reviewID := r.FormValue("id")
	if reviewID == "" {
		http.Error(w, "id를 설정해주세요", http.StatusBadRequest)
		return
	}
	rcp.ID = reviewID
	name := r.FormValue("name")
	if name == "" {
		http.Error(w, "name 을 설정해주세요", http.StatusBadRequest)
		return
	}
	rcp.Name = name
	err = SetReviewName(session, rcp.ID, rcp.Name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// json 으로 결과 전송
	data, err := json.Marshal(rcp)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// handleAPISetReviewPath 함수는 리뷰에서 Path를 설정합니다.
func handleAPISetReviewPath(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Post Only", http.StatusMethodNotAllowed)
		return
	}
	type Recipe struct {
		ID     string `json:"id"`
		Path   string `json:"path"`
		UserID string `json:"userid"`
	}
	rcp := Recipe{}
	session, err := mgo.Dial(*flagDBIP)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer session.Close()
	rcp.UserID, _, err = TokenHandler(r, session)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	r.ParseForm() // 받은 문자를 파싱합니다. 파싱되면 map이 됩니다.
	reviewID := r.FormValue("id")
	if reviewID == "" {
		http.Error(w, "id를 설정해주세요", http.StatusBadRequest)
		return
	}
	rcp.ID = reviewID
	path := r.FormValue("path")
	if path == "" {
		http.Error(w, "path 를 설정해주세요", http.StatusBadRequest)
		return
	}
	rcp.Path = path
	err = SetReviewPath(session, rcp.ID, rcp.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// json 으로 결과 전송
	data, err := json.Marshal(rcp)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// handleAPISetReviewCreatetime 함수는 리뷰에서 Createtime을 설정합니다.
func handleAPISetReviewCreatetime(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Post Only", http.StatusMethodNotAllowed)
		return
	}
	type Recipe struct {
		ID         string `json:"id"`
		Createtime string `json:"createtime"`
		UserID     string `json:"userid"`
	}
	rcp := Recipe{}
	session, err := mgo.Dial(*flagDBIP)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer session.Close()
	rcp.UserID, _, err = TokenHandler(r, session)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	r.ParseForm() // 받은 문자를 파싱합니다. 파싱되면 map이 됩니다.
	reviewID := r.FormValue("id")
	if reviewID == "" {
		http.Error(w, "id를 설정해주세요", http.StatusBadRequest)
		return
	}
	rcp.ID = reviewID
	createtime := r.FormValue("createtime")
	if createtime == "" {
		http.Error(w, "createtime 를 설정해주세요", http.StatusBadRequest)
		return
	}
	// RFC3339 타입인지 체크한다.
	_, err = time.Parse(time.RFC3339, createtime)
	if err != nil {
		http.Error(w, "createtime 값이 RFC3339 형태(2021-02-08T16:34:11+09:00)가 아닙니다", http.StatusBadRequest)
		return
	}
	rcp.Createtime = createtime
	err = SetReviewCreatetime(session, rcp.ID, rcp.Createtime)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	err = SetReviewUpdatetime(session, rcp.ID, rcp.Createtime)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// json 으로 결과 전송
	data, err := json.Marshal(rcp)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// handleAPISetReviewMainVersion 함수는 리뷰에서 MainVersion을 설정합니다.
func handleAPISetReviewMainVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Post Only", http.StatusMethodNotAllowed)
		return
	}
	type Recipe struct {
		ID          string `json:"id"`
		MainVersion int    `json:"mainversion"`
		UserID      string `json:"userid"`
	}
	rcp := Recipe{}
	session, err := mgo.Dial(*flagDBIP)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer session.Close()
	rcp.UserID, _, err = TokenHandler(r, session)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	r.ParseForm() // 받은 문자를 파싱합니다. 파싱되면 map이 됩니다.
	reviewID := r.FormValue("id")
	if reviewID == "" {
		http.Error(w, "id를 설정해주세요", http.StatusBadRequest)
		return
	}
	rcp.ID = reviewID
	mainVersion := r.FormValue("mainversion")
	if mainVersion == "" {
		http.Error(w, "mainversion 이 빈 문자열입니다", http.StatusBadRequest)
		return
	}
	rcp.MainVersion, err = strconv.Atoi(mainVersion)
	if err != nil {
		http.Error(w, "mainversion 을 설정해주세요", http.StatusBadRequest)
		return
	}
	err = SetReviewMainVersion(session, rcp.ID, rcp.MainVersion)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// json 으로 결과 전송
	data, err := json.Marshal(rcp)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// handleAPISetReviewSubVersion 함수는 리뷰에서 SubVersion을 설정합니다.
func handleAPISetReviewSubVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Post Only", http.StatusMethodNotAllowed)
		return
	}
	type Recipe struct {
		ID         string `json:"id"`
		SubVersion int    `json:"subversion"`
		UserID     string `json:"userid"`
	}
	rcp := Recipe{}
	session, err := mgo.Dial(*flagDBIP)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer session.Close()
	rcp.UserID, _, err = TokenHandler(r, session)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	r.ParseForm() // 받은 문자를 파싱합니다. 파싱되면 map이 됩니다.
	reviewID := r.FormValue("id")
	if reviewID == "" {
		http.Error(w, "id를 설정해주세요", http.StatusBadRequest)
		return
	}
	rcp.ID = reviewID
	subVersion := r.FormValue("subversion")
	if subVersion == "" {
		http.Error(w, "subversion 이 빈 문자열입니다", http.StatusBadRequest)
		return
	}
	rcp.SubVersion, err = strconv.Atoi(subVersion)
	if err != nil {
		http.Error(w, "subversion 을 설정해주세요", http.StatusBadRequest)
		return
	}
	err = SetReviewSubVersion(session, rcp.ID, rcp.SubVersion)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// json 으로 결과 전송
	data, err := json.Marshal(rcp)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// handleAPISetReviewFps 함수는 리뷰에서 Fps를 설정합니다.
func handleAPISetReviewFps(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Post Only", http.StatusMethodNotAllowed)
		return
	}
	type Recipe struct {
		ID     string  `json:"id"`
		Fps    float64 `json:"fps"`
		UserID string  `json:"userid"`
	}
	rcp := Recipe{}
	session, err := mgo.Dial(*flagDBIP)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer session.Close()
	rcp.UserID, _, err = TokenHandler(r, session)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	r.ParseForm() // 받은 문자를 파싱합니다. 파싱되면 map이 됩니다.
	reviewID := r.FormValue("id")
	if reviewID == "" {
		http.Error(w, "id를 설정해주세요", http.StatusBadRequest)
		return
	}
	rcp.ID = reviewID
	fps := r.FormValue("fps")
	if fps == "" {
		http.Error(w, "fps가 빈 문자열입니다", http.StatusBadRequest)
		return
	}
	rcp.Fps, err = strconv.ParseFloat(fps, 64)
	if err != nil {
		http.Error(w, "fps를 설정해주세요", http.StatusBadRequest)
		return
	}
	err = SetReviewFps(session, rcp.ID, rcp.Fps)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// json 으로 결과 전송
	data, err := json.Marshal(rcp)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// handleAPISetReviewDescription 함수는 리뷰에서 Description을 설정합니다.
func handleAPISetReviewDescription(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Post Only", http.StatusMethodNotAllowed)
		return
	}
	type Recipe struct {
		ID          string `json:"id"`
		Description string `json:"description"`
		UserID      string `json:"userid"`
	}
	rcp := Recipe{}
	session, err := mgo.Dial(*flagDBIP)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer session.Close()
	rcp.UserID, _, err = TokenHandler(r, session)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	r.ParseForm() // 받은 문자를 파싱합니다. 파싱되면 map이 됩니다.
	reviewID := r.FormValue("id")
	if reviewID == "" {
		http.Error(w, "id를 설정해주세요", http.StatusBadRequest)
		return
	}
	rcp.ID = reviewID
	rcp.Description = r.FormValue("description")
	err = SetReviewDescription(session, rcp.ID, rcp.Description)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// json 으로 결과 전송
	data, err := json.Marshal(rcp)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// handleAPISetReviewCameraInfo 함수는 리뷰에서 CameraInfo를 설정합니다.
func handleAPISetReviewCameraInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Post Only", http.StatusMethodNotAllowed)
		return
	}
	type Recipe struct {
		ID         string `json:"id"`
		CameraInfo string `json:"camerainfo"`
		UserID     string `json:"userid"`
	}
	rcp := Recipe{}
	session, err := mgo.Dial(*flagDBIP)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer session.Close()
	rcp.UserID, _, err = TokenHandler(r, session)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	r.ParseForm() // 받은 문자를 파싱합니다. 파싱되면 map이 됩니다.
	reviewID := r.FormValue("id")
	if reviewID == "" {
		http.Error(w, "id를 설정해주세요", http.StatusBadRequest)
		return
	}
	rcp.ID = reviewID
	rcp.CameraInfo = r.FormValue("camerainfo")
	err = SetReviewCameraInfo(session, rcp.ID, rcp.CameraInfo)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// json 으로 결과 전송
	data, err := json.Marshal(rcp)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// handleAPIReviewOutputDataPath 함수는 리뷰에서 OutputDataPath를 설정합니다.
func handleAPIReviewOutputDataPath(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPatch: // 많은 값 중에서 하나의 값만 바꾸기 때문에 PATCH를 사용했다. 과거에는 GET, POST만 사용했다. 특이사항이지만 미래적으로 PATCH, PUT과 함께 사용할 예정이다.
		type Recipe struct {
			ID             string `json:"id"`
			OutputDataPath string `json:"outputdatapath"`
			UserID         string `json:"userid"`
		}
		rcp := Recipe{}
		session, err := mgo.Dial(*flagDBIP)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer session.Close()
		rcp.UserID, _, err = TokenHandler(r, session)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		r.ParseForm()
		reviewID := r.FormValue("id")
		if reviewID == "" {
			http.Error(w, "id를 설정해주세요", http.StatusBadRequest)
			return
		}
		rcp.ID = reviewID
		rcp.OutputDataPath = r.FormValue("outputdatapath")
		err = SetReviewOutputDataPath(session, rcp.ID, rcp.OutputDataPath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		data, err := json.Marshal(rcp)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write(data)
	default:
		http.Error(w, "Patch Only", http.StatusMethodNotAllowed)
		return
	}
}

// handleAPISetReviewProcessStatus 함수는 리뷰에서 ProcessStatus를 설정합니다.
func handleAPISetReviewProcessStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Post Only", http.StatusMethodNotAllowed)
		return
	}
	type Recipe struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		UserID string `json:"userid"`
	}
	rcp := Recipe{}
	session, err := mgo.Dial(*flagDBIP)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer session.Close()
	rcp.UserID, _, err = TokenHandler(r, session)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	r.ParseForm() // 받은 문자를 파싱합니다. 파싱되면 map이 됩니다.
	reviewID := r.FormValue("id")
	if reviewID == "" {
		http.Error(w, "id를 설정해주세요", http.StatusBadRequest)
		return
	}
	rcp.ID = reviewID
	status := r.FormValue("status")
	if status == "" {
		http.Error(w, "status를 설정해주세요", http.StatusBadRequest)
		return
	}
	rcp.Status = status
	err = setReviewProcessStatus(session, rcp.ID, rcp.Status)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// json 으로 결과 전송
	data, err := json.Marshal(rcp)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// handleAPIUploadReviewDrawing 함수는 리뷰 드로잉이미지를 업로드 하는 RestAPI 이다.
func handleAPIUploadReviewDrawing(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Post Only", http.StatusMethodNotAllowed)
		return
	}
	type Recipe struct {
		Data   Review `json:"data"`
		UserID string `json:"userid"`
	}
	rcp := Recipe{}
	session, err := mgo.Dial(*flagDBIP)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer session.Close()
	rcp.UserID, _, err = TokenHandler(r, session)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	// 폼을 분석한다.
	err = r.ParseMultipartForm(int64(CachedAdminSetting.MultipartFormBufferSize))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	id := r.FormValue("id")
	if id == "" {
		http.Error(w, "id를 설정해주세요", http.StatusBadRequest)
		return
	}
	rcp.Data.ID = bson.ObjectIdHex(id)
	sktch := Sketch{}
	// 프레임 설정
	frame := r.FormValue("frame")
	if frame == "" {
		http.Error(w, "frame을 설정해주세요", http.StatusBadRequest)
		return
	}
	sktch.Frame, err = strconv.Atoi(frame)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// Duration 설정
	durationStr := r.FormValue("duration")
	if durationStr == "" {
		durationStr = "1"
	}
	sktch.Duration, err = strconv.Atoi(durationStr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// 파일체크
	if len(r.MultipartForm.File) == 0 { // 파일이 없다면 에러처리한다.
		http.Error(w, "드로잉 이미지를 설정해주세요", http.StatusBadRequest)
		return
	}
	if len(r.MultipartForm.File) != 1 { // 파일이 복수일 때
		http.Error(w, "드로잉 이미지가 여러개 설정되어있습니다", http.StatusBadRequest)
		return
	}
	// 썸네일이 존재한다면 썸네일을 처리한다.
	for _, files := range r.MultipartForm.File {
		for _, f := range files {
			if f.Size == 0 {
				http.Error(w, "파일사이즈가 0 바이트입니다", http.StatusBadRequest)
				return
			}
			file, err := f.Open()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				continue
			}
			defer file.Close()
			switch f.Header.Get("Content-Type") {
			case "image/jpeg", "image/png":
				data, err := ioutil.ReadAll(file)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				// 썸네일 이미지가 이미 존재하는 경우 이미지 파일을 지운다.
				imgPath := fmt.Sprintf("%s/%s.%06d.png", CachedAdminSetting.ReviewDataPath, id, sktch.Frame)
				if _, err := os.Stat(imgPath); os.IsExist(err) {
					err = os.Remove(imgPath)
					if err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
				}
				// 사용자가 업로드한 데이터를 이미지 자료구조로 만들고 리사이즈 한다.
				img, _, err := image.Decode(bytes.NewReader(data)) // 전송된 바이트 파일을 이미지 자료구조로 변환한다.
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				err = imaging.Save(img, imgPath)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				sktch.SketchPath = imgPath
				sktch.Author = rcp.UserID
				sktch.Updatetime = time.Now().Format(time.RFC3339)
			default:
				http.Error(w, "허용하지 않는 파일 포맷입니다", http.StatusBadRequest)
				return
			}
		}
	}
	review, err := getReview(session, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	hasFrame := false
	for _, s := range review.Sketches {
		if s.Frame == sktch.Frame {
			hasFrame = true
		}
	}
	if !hasFrame {
		review.Sketches = append(review.Sketches, sktch)
		// Frame 순서로 스케치정보를 정렬한다.
		sort.SliceStable(review.Sketches, func(i, j int) bool {
			return review.Sketches[i].Frame < review.Sketches[j].Frame
		})
		err = setReviewItem(session, review)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		rcp.Data = review
	}
	data, err := json.Marshal(rcp)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// handleAPIRmReviewDrawing 함수는 리뷰 드로잉이미지를 삭제하는 RestAPI 이다.
func handleAPIRmReviewDrawing(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Post Only", http.StatusMethodNotAllowed)
		return
	}
	type Recipe struct {
		ID     string `json:"id"`
		Frame  int    `json:"frame"`
		UserID string `json:"userid"`
	}
	rcp := Recipe{}
	session, err := mgo.Dial(*flagDBIP)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer session.Close()
	rcp.UserID, _, err = TokenHandler(r, session)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	r.ParseForm()
	id := r.FormValue("id")
	if id == "" {
		http.Error(w, "id를 설정해주세요", http.StatusBadRequest)
		return
	}
	rcp.ID = id
	frame := r.FormValue("frame")
	if frame == "" {
		http.Error(w, "frame을 설정해주세요", http.StatusBadRequest)
		return
	}
	rcp.Frame, err = strconv.Atoi(frame)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// DB에 저장된 드로잉 프레임을 제거한다.
	review, err := getReview(session, rcp.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	newSketchs := []Sketch{}
	for _, s := range review.Sketches {
		if s.Frame != rcp.Frame {
			newSketchs = append(newSketchs, s)
		}
	}
	review.Sketches = newSketchs
	err = setReviewItem(session, review)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 썸네일 이미지가 존재한다면 이미지 파일을 지운다.
	imgPath := fmt.Sprintf("%s/%s.%06d.png", CachedAdminSetting.ReviewDataPath, rcp.ID, rcp.Frame)
	err = os.Remove(imgPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	data, err := json.Marshal(rcp)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// handleAPIReviewDrawingFrame 함수는 리뷰 드로잉 프레임 정보를 가지고 오는 RestAPI 이다.
func handleAPIReviewDrawingFrame(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Post Only", http.StatusMethodNotAllowed)
		return
	}
	type Recipe struct {
		ID          string `json:"id"`
		Frame       int    `json:"frame"`
		ResultFrame int    `json:"resultframe"`
		Mode        string `json:"mode"`
		UserID      string `json:"userid"`
	}
	rcp := Recipe{}
	session, err := mgo.Dial(*flagDBIP)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer session.Close()
	rcp.UserID, _, err = TokenHandler(r, session)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	r.ParseForm()
	id := r.FormValue("id")
	if id == "" {
		http.Error(w, "id를 설정해주세요", http.StatusBadRequest)
		return
	}
	rcp.ID = id
	mode := r.FormValue("mode")
	if !(mode == "prev" || mode == "next") {
		http.Error(w, "mode는 prev, next만 지원합니다", http.StatusBadRequest)
		return
	}
	rcp.Mode = mode
	frame := r.FormValue("frame")
	if frame == "" {
		http.Error(w, "frame을 설정해주세요", http.StatusBadRequest)
		return
	}
	rcp.Frame, err = strconv.Atoi(frame)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// DB에 저장된 드로잉 프레임을 제거한다.
	review, err := getReview(session, rcp.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var frames []int
	for _, n := range review.Sketches {
		frames = append(frames, n.Frame)
	}
	if len(frames) == 0 {
		http.Error(w, "드로잉 프레임이 존재하지 않습니다", http.StatusBadRequest)
		return
	}
	if rcp.Mode == "prev" {
		frames = ReverseIntSlice(frames)
	}
	for _, n := range frames {
		if rcp.Mode == "prev" {
			if n < rcp.Frame {
				rcp.ResultFrame = n
				break
			}
		} else {
			if n > rcp.Frame {
				rcp.ResultFrame = n
				break
			}
		}
	}
	// 만약 조건에 맞지않는 프레임이 입력되면 UX에서 frame이 순환할 수 있도록 존재하는 프레임을 리턴한다.
	if rcp.ResultFrame == 0 {
		rcp.ResultFrame = frames[0]
	}
	data, err := json.Marshal(rcp)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// handleAPISetReviewAgainForWaitStatusToday 함수는 Wait 상태의 리뷰 데이터를 오늘 리뷰할 수 있도록 날짜를 바꾼다.
func handleAPISetReviewAgainForWaitStatusToday(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Post Only", http.StatusMethodNotAllowed)
		return
	}
	type Recipe struct {
		UserID string `json:"userid"`
		Num    int    `json:"num"`
	}
	rcp := Recipe{}
	session, err := mgo.Dial(*flagDBIP)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer session.Close()
	rcp.UserID, _, err = TokenHandler(r, session)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	_, _, err = net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	// 리뷰 대기상태를 불러온다.
	reviews, err := searchReview(session, "status:wait")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// 오늘 리뷰 데이터로 변환할 갯수를 구한다.
	rcp.Num = len(reviews)
	// 리뷰 대기항목을 오늘 날짜로 변환한다.
	today := time.Now().Format(time.RFC3339)
	for _, review := range reviews {
		err = SetReviewCreatetime(session, review.ID.Hex(), today)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	data, err := json.Marshal(rcp)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}
