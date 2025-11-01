package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"runtime"
	"strconv"

	"github.com/spf13/cobra"
)

type Cookie struct {
	RevelSession string `json:"REVEL_SESSION"`
	Ga           string `json:"_ga"`
	Ga_          string `json:"_ga_RC512FD18N"`
	TimeDelta    string `json:"timeDelta"`
	RevelFlash   string `json:"REVEL_FLASH"`
}

var (
	getProblemsPageURL = "https://atcoder.jp/contests/abc"
	CookieKey          = "cookie"
)

func main() {

	ctx := context.Background()

	rootCmd := &cobra.Command{
		Use:   "ac",
		Short: "CLI tool for downloading atcoder problems and tests and also running tests for it.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
	}

	rootCmd.AddCommand(downloadCmd(ctx))
	rootCmd.AddCommand(runTestsCmd(ctx))

	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}

func downloadCmd(ctx context.Context) *cobra.Command {

	var contestNumber int

	downloadCmd := &cobra.Command{
		Use:   "dw",
		Short: "Used for downloading problems and test with them in specilized directories",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {

			// Check os
			switch os := runtime.GOOS; os {
			case "darwin":
				fmt.Println("Hi from darwin side")
			}

			// Open file, get cookies
			fileName := "cookie.json"
			flags := os.O_RDONLY
			perm := os.FileMode(0400) // Read only permission

			file, err := os.OpenFile(fileName, flags, perm)
			if err != nil {
				return err
			}

			defer file.Close()

			// Read that shit in a buffer
			body, err := io.ReadAll(file)
			if err != nil {
				return err
			}

			// Init a struct, read the body there. Thats why we have json tags in struct
			var cookie Cookie
			err = json.Unmarshal(body, &cookie)
			if err != nil {
				return err
			}

			// If successful, store it in context as a value and get the value from context when required
			ctx = context.WithValue(ctx, CookieKey, cookie)

			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {

			// Download the html files, look for testcases, create new directories, set them up
			// We have the contest number with us, send it now in functions that will take care of it

			err := DownloadAndLoadProblems(ctx, contestNumber)

			return err
		},
	}

	downloadCmd.Flags().IntVarP(&contestNumber, "contest", "c", 1, "used to give contest number")

	return downloadCmd
}

func runTestsCmd(ctx context.Context) *cobra.Command {

	runTestsCmd := &cobra.Command{
		Use:   "jj",
		Short: "Run tests with help of this",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// Security check like dirs and whatever exists or not and so on
			return nil
		},
	}

	return runTestsCmd
}

func DownloadAndLoadProblems(ctx context.Context, contestNumber int) error {

	// First check how many problems are there, then proceed to download their data(html), parse it, and create a A.cpp file and multiple testcases file in their respective directories
	problemNumber, err := GetNumberOfProblems(ctx, contestNumber)
	if err != nil {
		return err
	}

	log.Printf("Problem number: %v", problemNumber)

	return nil
}

func GetNumberOfProblems(ctx context.Context, contestNumber int) (int, error) {

	currentContestPageURL := getProblemsPageURL + strconv.Itoa(contestNumber)

	client := http.Client{}
	req, err := http.NewRequestWithContext(ctx, "GET", currentContestPageURL, nil)
	if err != nil {
		return 0, err
	}

	// Get cookie value from context
	cookieVals, ok := ctx.Value(CookieKey).(Cookie)
	if !ok {
		return 0, fmt.Errorf("cookie values not found in context")
	}

	req.AddCookie(&http.Cookie{Name: "REVEL_SESSION", Value: cookieVals.RevelSession})
	req.AddCookie(&http.Cookie{Name: "_ga", Value: cookieVals.Ga})
	req.AddCookie(&http.Cookie{Name: "_ga_RC512FD18N", Value: cookieVals.Ga_})
	req.AddCookie(&http.Cookie{Name: "timeDelta", Value: cookieVals.TimeDelta})
	req.AddCookie(&http.Cookie{Name: "REVEL_FLASH", Value: cookieVals.RevelFlash})

	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("unexpected status code %d while fetching contest page %q", resp.StatusCode, currentContestPageURL)
	}

	html, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}

	taskScoreRegex := regexp.MustCompile(`<td style="text-align:center">([A-Z])</td>\s*<td style="text-align:center">(\d+)</td>`)
	matches := taskScoreRegex.FindAllStringSubmatch(string(html), -1)

	if len(matches) == 0 {
		return 0, fmt.Errorf("could not find any task rows in contest page %q", currentContestPageURL)
	}

	uniqueTasks := make(map[string]struct{})
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		task := match[1]
		if len(task) != 1 {
			continue
		}
		if _, exists := uniqueTasks[task]; !exists {
			uniqueTasks[task] = struct{}{}
		}
	}

	if len(uniqueTasks) == 0 {
		return 0, fmt.Errorf("parsed zero unique tasks from contest page %q", currentContestPageURL)
	}

	return len(uniqueTasks), nil
}
