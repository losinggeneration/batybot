# Batybot

Batybot is a basic Twitch bot for running on JilliiiBeanz's channel.

# Environment

The following settings can be used:

    TWITCH_TOKEN     - An oauth token in the format: oauth:TOKEN
    TWITCH_USER      - username to login as.
    TWITCH_CHANNEL   - the channel (one for now) that the bot should join
    TWITCH_CLIENT_ID - used to get the auth token with the twitch cli

# Getting an oauth token

In order to use the bot it needs pretty much full privileges.

    twitch-cli token -u --client-id=$TWITCH_CLIENT_ID -s "chat:edit chat:read whispers:read whispers:edit"

If it's made mod, it can omit the whispers permissions.

    twitch-cli token -u --client-id=$TWITCH_CLIENT_ID -s "chat:edit chat:read"
