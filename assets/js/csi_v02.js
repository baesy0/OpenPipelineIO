var OSName="Linux";
if (navigator.appVersion.indexOf("Win") != -1) OSName="Win";
if (navigator.appVersion.indexOf("Mac") != -1) OSName="Mac";
if (navigator.appVersion.indexOf("X11") != -1) OSName="Linux";
if (navigator.appVersion.indexOf("Linux") != -1) OSName="Linux";

// changeProject 함수는 검색바의 프로젝트가 바뀔 때 발생하는 이벤트를 처리한다.
function changeProject() {
	// 제목설정
	let pretitle = "OPIP: "
	let e = document.getElementById("searchbox-project");
	let project = e.options[e.selectedIndex].value;
	document.title = pretitle + project;
	// 검색에 사용되는 Template 수정하기. 모든 프로젝트가 ver2를 사용하게되면 이 코드는 레거시가 된다.
	fetch('/api/project?id='+project, {
        method: 'GET',
        headers: {
            "Authorization": "Basic "+ document.getElementById("token").value,
        },
    })
    .then((response) => {
        return response.json()
    })
    .then((data) => {
		
    })
    .catch((error) => {
        alert(error)
    });
}




// selectmodeV2 함수는 검색바에서 "선택모드" 버튼을 눌렀을 때 실행되는 함수이다.
function selectmodeV2() {
	var checknum = 0
	// 체크된 Status 수를 구한다.
	for(var i=0; i<document.getElementsByClassName('StatusCheckBox').length;i++) {
		if (document.getElementsByClassName('StatusCheckBox')[i].checked == true) {
			checknum += 1
		}
	}
	// 다 켜져있을 때는 다 끈다.
	if (checknum == document.getElementsByClassName('StatusCheckBox').length) {
		for(var i=0; i<document.getElementsByClassName('StatusCheckBox').length;i++) {
			document.getElementsByClassName('StatusCheckBox')[i].checked=false
		}
	} else if (checknum == 0) {
		// 다 꺼져 있을 때는 몇개만 켠다.
		for(var i=0; i<document.getElementsByClassName('StatusCheckBox').length;i++) {
			document.getElementsByClassName('StatusCheckBox')[i].checked=false
		}
		for(var i=0; i<document.getElementsByClassName('DefaultStatusCheckBox').length;i++) {
			document.getElementsByClassName('DefaultStatusCheckBox')[i].checked=true
		}
	} else {
		// 일부만 켜있다면 다 켠다.
		for(var i=0; i<document.getElementsByClassName('StatusCheckBox').length;i++) {
			document.getElementsByClassName('StatusCheckBox')[i].checked=true
		}
	}
}


//샷 체크박스 F5 눌렀을때 없애는 기능.
function uncheck() {
	var checkboxes = document.getElementsByName('select_slug');
	for (var i=0; i<checkboxes.length; i++) {
		console.log(checkboxes[i].type)
		if (checkboxes[i].type == 'checkbox') {
			checkboxes[i].checked = false;
		}
	}
}


function playmov(address)
{
	myWindow=window.open(address,"PlayWindows","width=1280, height=720, toolbar=no, menubar=no, location=no");
	myWindow.focus();
}

function keypressed(){
	if(event.keyCode==122) self.close();
	else return false;
}
document.omkeydown=keypressed;


function onlyNumber(event) {
	event = event || window.event;
	var keyID = (event.which) ? event.which : event.keyCode;
	if ( (keyID >=48 && keyID <= 57) || (keyID >= 96 && keyID <= 105) || keyID == 8 || keyID == 46 || keyID == 37 || keyID == 39)
		return;
	else
		return false;
}

function removeChar(event) {
	event = event || window.event;
	var keyID = (event.which) ? event.which : event.keyCode;
	if (keyID == 8 || keyID == 46 || keyID == 37 || keyID == 39)
		return;
	else
		event.target.value = event.target.value.replace(/[^0-9]/g,"");
}


function removeWhiteSpace(event) {
	event.value = event.value.replace(/ /g, '');
}

// *문자를 x문자로 바꾼다.
// X를 x문자로 바꾼다.
// 공백을 제거한다.
// 렌즈디스토션값을 입력시 2048*1280 -> 2048x1280 형태로 바꾸기 위함이다.
// 숫자와 x를 제외한 영문입력시 삭제됩니다.
function widthxHeight(event) {
	event = event || window.event;
	event.target.value = event.target.value.replace("*","x");
	event.target.value = event.target.value.replace("X","x");
	event.target.value = event.target.value.replace(/[^\d\x]/gi,"");
}

//drop된 file의 "file://" 제거
function rmFileProtocol(event) {	
	event = event || window.event;
	event.preventDefault();
	
	var data= event.dataTransfer.getData('text/plain'); //드래그한 데이터 자료를 얻는다.
	event.target.value = "";
	event.target.value = data.replace("file://","");
}

//버튼을 클릭 하면 editItem 언디스토션사이즈 form에 placesize(scansize) 값을 입력한다.
//Checkbox를 체크하면 undistort value에 platesize가 입력된다.
//Checkbox의 체크를 해제하면 undistort value가 ""이 된다.
function inputNone(checkbox) {
	if (checkbox.checked) {
		document.getElementById("undistort").value = document.getElementById("platesize").value;
	} else {
		document.getElementById("undistort").value = "";
	}
}

// ScreenX 버튼이 클릭될때 체크 여부에 따라 이벤트가 발생한다.
// ScreenX가 체크되면 ScreenxOverlay가 활성화 된다.
// ScreenX가체크가 해제되면 ScreenxOverlay가 비활성화되고 1.0의 값이 들어간다.
function checkScreenx(event) {
	event = event || window.event;
	if (event.target.checked) {
		document.getElementById("screenxoverlay").readOnly = false;
	} else {
		document.getElementById("screenxoverlay").readOnly = true;
		document.getElementById("screenxoverlay").value = 1.0;
	}
}


function wfs(host, task, type, assettype, project, name, seq, cut, token) {
	let WFSPATH = "";
	$.ajax({
		url: "/api/tasksetting",
		type: "post",
		data: {
			os: "", // os를 설정하지 않으면 WFSPath를 불러온다.
			task: task,
			type: type,
			assettype: assettype,
			project: project,
			name: name,
			seq: seq,
			cut: cut,
		},
		headers: {
			"Authorization": "Basic "+ token
		},
		async:false,
		dataType: "json",
		success: function(data) {
			WFSPATH = data.path
		},
		error: function(request,status,error){
			alert("code:"+request.status+"\n"+"status:"+status+"\n"+"Msg:"+request.responseText+"\n"+"error:"+error);
		}
	});
	window.open(host + WFSPATH, 'WebFileSystem', 'left=20, top=20, width=500, height=500, toolbar=0, resizable=1, scrollbars=1').focus();
}
	