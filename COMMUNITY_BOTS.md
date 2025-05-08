# Community Bots

The Bacalhau Bluesky Bot supports bot creations from members of the community!

If you create a bot using this guide, you can execute applications to interact with and respond to posts mentioning your bot on Bluesky

## Overview

Running a bot with Bacalhau is simple! Any containerised application you build can be configured as the backend that will execute for any post mentioning your bots account.

Community bots will be assigned a handle `@<YOUR_BOT_NAME>.bacalhau.org`. Any post mentioning this account will invoke your application code and run it on the Expanso Bacalhau Bot Network.

The Expanso Bacalhau Bot Network are a series of distributed compute nodes that are operated by Expanso and give your code a place to run. If someone mentions your bot's account, that post will be processed and passed through to your bot with a number of environment variables enabling you to process text and images, and set a response.

Each bot that runs on the Expanso Bacalhau Bot Network will be passed the following values as environment variables on invocation for processing:

`POST` - The raw post text with mentions and links
`FROM` - The handle of the account that mentioned your bot in the invoking post
`IMAGES` - An array of JSON objects describing any images included in the post that invoked your bot
`PROCESSED_POST` - A string value with only the post text of the invoking post, with all account handles replaced.
`WHOAMI` - Your bot's Bluesky account handle.

## Dive in

To get you started quickly, we've built a [demo bot](https://bsky.app/profile/calculator.bots.bacalhau.org) - a calculator - which responds to posts with mathematical equations, and replies with the response.

You can find the code for the bot [here](https://github.com/bacalhau-project/bacalhau-bluesky-bot-calculator) which can you use as a scaffold to get things started quickly.

Clone the repo to your system and take a look at the files therein. You'll see that it's just an ordinary, simple Python script with a couple of dependencies described in requirements.txt.

```bash
├── Dockerfile
├── LICENSE
├── README.md
├── main.py
└── requirements.txt
```

1. `main.py` - where our bots code lives. This will be invoked and have information about the invoking post passed through to it with environment variables
2. `Dockerfile` - a standard Dockerfile for containerising and running our bot application code.
3. `requirements.txt` - The list of dependencies that will be bundled with our application on the building of the image.

### The Demo Bot Code

Let's take a look at the `main.py` file:

```python
import os
from sympy import sympify

WHOAMI = os.environ.get("WHOAMI")
POST_TEXT = os.environ.get("POST")
FROM = os.environ.get("FROM")
PROCESSED_POST = os.environ.get("PROCESSED_POST", "")

def evaluate_expression(expr):
    try:
        result = sympify(expr)
        decimal_result = result.evalf()
        return round(decimal_result, 2)
    except Exception as e:
        return 


def main():

    result = evaluate_expression(PROCESSED_POST)

    if result is None:
        print("🔥🧮🔥")
    else:
        print(result)    

if __name__ == "__main__":
    main()
```

If a Bluesky account mentions your bot, the code for your bot will be executed and have information about the post passed through to it through environment variables.

In this case of the calculator bot, you can see that we only want to handle the `PROCESSED_POST` property that's passed through to our code, which we can then evaluate and return the result.

Bacalhau Bluesky Bots read the `stdout` of the applications that are running, and then uses that as the response to the original post - if one is warranted. You can see in the above code, that if our code has returned a calculation based on the Bluesky post that invoked it, we print out that result for the Bacalhau Bluesky Bot Network to pick and and post as a response to the original poster. If there's an error in our application, or if the original post was not a valid mathematical expression, we print out `🔥🧮🔥` instead to show that something went wrong.

## Adding your own Bot to the Bacalhau Network

Once you have your bot code written, containerised, and added to a container registry, you can add a pull request to the [Bacalhau Bluesky Bot repo](https://github.com/bacalhau-project/bacalhau-bluesky-bot) to kickstart the approval and account account creation process.

To do this, you'll need to fork the Bacalhau Bluesky Bot project, and create a new directory in the `./community` folder of the repo with your bots desired name.

You can see an example of the Calculator bot's `job.yaml` and `info.yaml` files [here]().

Inside this directory, you will need to include two file:

1. `job.yaml` - A Bacalhau Job YAML file which describes where your containerised bot code is, and the resources needed to run it.
2. `info.json` - A JSON file which includes metadata for the bot.

### job.yaml
The `job.yaml` file is just a standard Bacalhau Job file. It contains all of the information that Bacalhau needs to run a Job (your bot code) on the Expanso Bacalhau Bot Network.

For example, our Calculator bot's YAML file looks like this:

```yaml
Name: calculator
Type: batch
Count: 1
Tasks:
  - Name: main
    Engine:
      Type: docker
      Params:
        Image: seanmtracey/bbb-calculator:latest
    Resources: 
      CPU: "1"
      Memory: "128MB"
```

This tells the Bacalhau Network that when our bot is being run, we need a system that has a least 1 CPU, 128MB of memory, and that our bot application's image can be found at `seanmtracey/bbb-calculator:latest` in Docker hub.

These are a minimum set of properties needed to run a Bacalhau Bluesky bot on the Expanso Bacalhau Bot Network.

### info.json

The `info.json` file contains metadata for our bot. This information is used by the Bacalhau Bot Network to:

- Identify your bot
- Determine whether or not it should be invoked
- What values should be sent upon invocation

The `info.json` file for our calculator looks like so:

```json
{
    "name" : "calculator",
    "storage" : false,
    "environmentVariables" : [
        "POST",
        "FROM",
        "IMAGES",
        "PROCESSED_POST"
    ],
    "type" : "mention",
    "repo" : "https://github.com/seanmtracey/bbb-calculator",
    "author" : "@seanmtracey.bsky.social"
}
```

Let's break down those properties...

`name` - This is the name that you would like your bot to have. It will be prepended to `bots.bacalhau.org` to make up your Bluesky bot's account handle. In the case of our calculator, this would be `calculator.bots.bacalhau.org`. This must be unique across all Bacalhau Bluesky Bots. 

`storage` - Whether or not you will need read/write access to a sandboxed directory on the Bacalhau Network. This is not implemented at this point, so for now the value is `false`.

`environmentVariables` - An array of environment variables you would like passed through to your bot upon invocation. If you omit a value from this array, then the corresponding information will not be passed to your bot on invocation.

`type` - The expected interaction your bot will be invoked for - at this time, the only type is `mention`.

`repo` - Where can we find the application code for your bot so that we can review it?

`author` - The Bluesky handle of the person who created this bot - presumably yours, but it could be someone else who will look after the bots code.

### Submitting your Bot for submission.

Once you've added those files your bot directory in the community folder of your fork of this repo, open up a PR and describe what it is your bot does.

We will review your application, and if we feel that it's a good fit, we'll create a Bluesky account for your bot to run under and enable it on the network.

It's worth noting that we're not giving community members access to the credentials of these accounts at this time, but if you have a particular need, or are stuck, raise an issue and let us know and we'll see what we can do.