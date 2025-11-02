package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
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

type cookieContextKey struct{}

var (
	getProblemsPageURL string = "https://atcoder.jp/contests/abc"
	cookieKey                 = cookieContextKey{}
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
	var directoryPath string

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

			defer func() {
				if err := file.Close(); err != nil {
					log.Fatal(err)
				}
			}()

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
			ctx = context.WithValue(ctx, cookieKey, cookie)

			// Get current working directory
			currDir, err := os.Getwd()
			if err != nil {
				return err
			}

			// Default directory path to current dir when flag not provided
			if directoryPath == "" {
				directoryPath = currDir
			} else if !filepath.IsAbs(directoryPath) {

				// Check if the directory actually exists, or else default to curr directory
				_, err := os.Stat(directoryPath)
				if err != nil {

					if errors.Is(err, os.ErrNotExist) {
						return fmt.Errorf("directory '%s' does not exists: %v", directoryPath, err)
					} else {
						return fmt.Errorf("error checking directory '%s': %v", directoryPath, err)
					}
				}

			}

			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {

			// Download the html files, look for testcases, create new directories, set them up
			// We have the contest number with us, send it now in functions that will take care of it

			err := DownloadAndLoadProblems(ctx, contestNumber, directoryPath)

			return err
		},
	}

	downloadCmd.Flags().IntVarP(&contestNumber, "contest", "c", 1, "used to give contest number")
	downloadCmd.Flags().StringVarP(&directoryPath, "path", "p", "", "used to express directory path (defaults to current directory)")

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

func DownloadAndLoadProblems(ctx context.Context, contestNumber int, directoryPath string) error {

	// First check how many problems are there, then proceed to download their data(html), parse it, and create a A.cpp file and multiple testcases file in their respective directories
	problemsNumber, err := GetNumberOfProblems(ctx, contestNumber)
	if err != nil {
		return err
	}

	// Now we will have to make that many directories, starting from A and so on. Lets take this directory as a flag
	// Loop over the problems and create directoriess.
	// First build the main directory i.e. the contest number

	directoryPath = filepath.Join(directoryPath, fmt.Sprintf("ABC%d", contestNumber))
	err = os.MkdirAll(directoryPath, 0755)
	if err != nil {
		return err
	}

	for i := 0; i < problemsNumber; i++ {
		alpha := string('A' + rune(i))

		newProblemDirectory := filepath.Join(directoryPath, alpha)
		err := os.MkdirAll(newProblemDirectory, 0755)
		if err != nil {
			return err
		}

	}

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
	cookieVals, ok := ctx.Value(cookieKey).(Cookie)
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
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Fatal(err)
		}
	}()

	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("unexpected status code %d while fetching contest page %q", resp.StatusCode, currentContestPageURL)
	}

	html, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}

	taskScoreRegex := regexp.MustCompile(`<td[^>]*>\s*([A-Z])\s*</td>\s*<td[^>]*>\s*(\d+)\s*</td>`)
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
