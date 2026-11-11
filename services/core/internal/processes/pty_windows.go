//go:build windows

package processes

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// RunPTYChild runs only in the Core's supervised helper process.
func RunPTYChild() error {
	encoded := os.Getenv("CLOUDFORGE_PTY_CHILD")
	configBytes, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return err
	}
	var config struct {
		Path string   `json:"path"`
		Args []string `json:"args"`
		Dir  string   `json:"dir"`
	}
	if err = json.Unmarshal(configBytes, &config); err != nil {
		return err
	}
	if config.Path == "" {
		return errors.New("comando do terminal vazio")
	}
	if config.Dir == "" {
		config.Dir, err = os.Getwd()
		if err != nil {
			return err
		}
	}

	var inputRead, inputWrite, outputRead, outputWrite windows.Handle
	if err = windows.CreatePipe(&inputRead, &inputWrite, nil, 0); err != nil {
		return err
	}
	defer func() {
		if inputRead != 0 {
			windows.CloseHandle(inputRead)
		}
		if inputWrite != 0 {
			windows.CloseHandle(inputWrite)
		}
	}()
	if err = windows.CreatePipe(&outputRead, &outputWrite, nil, 0); err != nil {
		return err
	}
	defer func() {
		if outputRead != 0 {
			windows.CloseHandle(outputRead)
		}
		if outputWrite != 0 {
			windows.CloseHandle(outputWrite)
		}
	}()
	var console windows.Handle
	if err = windows.CreatePseudoConsole(windows.Coord{X: 100, Y: 30}, inputRead, outputWrite, 0, &console); err != nil {
		return err
	}
	defer func() {
		if console != 0 {
			windows.ClosePseudoConsole(console)
		}
	}()
	// Keep the input read handle open: closing it here can prevent ConHost from receiving input.
	windows.CloseHandle(outputWrite)
	outputWrite = 0
	output := os.NewFile(uintptr(outputRead), "conpty-output")
	copyDone := make(chan struct{})
	go func() { _, _ = io.Copy(os.Stdout, output); close(copyDone) }()

	attributes, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return err
	}
	defer attributes.Delete()
	if err = attributes.Update(windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, unsafe.Pointer(console), unsafe.Sizeof(console)); err != nil {
		return err
	}
	startup := windows.StartupInfoEx{}
	startup.Cb = uint32(unsafe.Sizeof(startup))
	startup.Flags = windows.STARTF_USESTDHANDLES
	startup.ProcThreadAttributeList = attributes.List()
	command, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(append([]string{config.Path}, config.Args...)))
	if err != nil {
		return err
	}
	dir, err := windows.UTF16PtrFromString(config.Dir)
	if err != nil {
		return err
	}
	var child windows.ProcessInformation
	if err = windows.CreateProcess(nil, command, nil, nil, false, windows.EXTENDED_STARTUPINFO_PRESENT|windows.CREATE_UNICODE_ENVIRONMENT|windows.CREATE_SUSPENDED, nil, dir, &startup.StartupInfo, &child); err != nil {
		return err
	}
	defer windows.CloseHandle(windows.Handle(child.Thread))
	defer windows.CloseHandle(windows.Handle(child.Process))
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		windows.TerminateProcess(windows.Handle(child.Process), 1)
		return err
	}
	defer func() {
		if job != 0 {
			windows.CloseHandle(job)
		}
	}()
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		windows.TerminateProcess(windows.Handle(child.Process), 1)
		return err
	}
	if err = windows.AssignProcessToJobObject(job, windows.Handle(child.Process)); err != nil {
		windows.TerminateProcess(windows.Handle(child.Process), 1)
		return err
	}
	if _, err = windows.ResumeThread(windows.Handle(child.Thread)); err != nil {
		windows.TerminateProcess(windows.Handle(child.Process), 1)
		return err
	}

	input := os.NewFile(uintptr(inputWrite), "conpty-input")
	go func() {
		decoder := json.NewDecoder(os.Stdin)
		for {
			var message struct {
				Type string `json:"type"`
				Data string `json:"data"`
				Cols int16  `json:"cols"`
				Rows int16  `json:"rows"`
			}
			if decoder.Decode(&message) != nil {
				return
			}
			switch message.Type {
			case "input":
				data, err := base64.StdEncoding.DecodeString(message.Data)
				if err == nil {
					_, _ = input.Write(data)
				}
			case "resize":
				if message.Cols >= 20 && message.Rows >= 5 {
					_ = windows.ResizePseudoConsole(console, windows.Coord{X: message.Cols, Y: message.Rows})
				}
			}
		}
	}()
	if _, err = windows.WaitForSingleObject(windows.Handle(child.Process), windows.INFINITE); err != nil {
		return err
	}
	// ConHost may still be flushing the final frame when the child exits.
	time.Sleep(500 * time.Millisecond)
	windows.CloseHandle(job)
	job = 0
	windows.ClosePseudoConsole(console)
	console = 0
	windows.CloseHandle(inputWrite)
	inputWrite = 0
	select {
	case <-copyDone:
	case <-time.After(2 * time.Second):
	}
	var exitCode uint32
	if err = windows.GetExitCodeProcess(windows.Handle(child.Process), &exitCode); err != nil {
		return err
	}
	if exitCode != 0 {
		return fmt.Errorf("processo terminou com código %d", exitCode)
	}
	return nil
}
