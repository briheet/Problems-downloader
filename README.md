# Welcome to Ac downloader

Hi! i am Briheet. This cli tools is on purpose to be in a single file paired with a json file.
I enjoy competitive programming in my free time and use Helix as my text editor.
Its usually very weird to just use vscode and do competitive programming when Helix is my primary editor.

Hence this cli tool. It helps me to download problems for a specific context, create a new directory, and have my test cases init there.
Altough its nowwhere close, i am hoping to finish it up in my free time i get after work (sundays only :sob) so i can enjoy solving problems again :)

## Requirements

You just need golang installed on our system. This is dev phase rn.

Have a atcoder account. Go to any contest landing page, go to network tab, you will have something as abc(contest id here).
Do right click, COPY AS CURL, and paste it somewhere. Now copy the cookies values in cookie.json file. That should be it.

## Build

1. Copy the cookie file.
```bash
cp cookie.example.json cookie.json
```
Now populate the cookie file

2. First compile the project. 
```bash
go build -ldflags "-s -w" -o ac main.go
./ac -h
```

## Run

1. To Download contest, give it the contest number
```bash
./ac dw -c 430
```
