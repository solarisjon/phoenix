# Set Up Telegram Notifications

Get notified on your phone when tasks fail, complete, or need approval.

## 1. Create a Telegram bot

1. Open Telegram and search for **@BotFather**
2. Send `/newbot`
3. Pick a name and username for your bot
4. BotFather gives you an **API token** — copy it

## 2. Get your chat ID

**Easy way:** Phoenix can discover it automatically — skip ahead to step 3 and click **Detect Chat ID** after entering your token. Send your bot any message first so the API has a recent update to read.

**Manual way:**
1. Start a conversation with your new bot (send it any message)
2. Open `https://api.telegram.org/bot<YOUR_TOKEN>/getUpdates` in a browser
3. Find the `chat.id` number in the response (e.g. `-1001234567890` for groups, or a positive number for DMs)

## 3. Configure in Phoenix

1. Set the environment variable: `export TELEGRAM_BOT_TOKEN="your-token-here"`
2. Open **Plugins** in the Phoenix sidebar
3. Enable **Core Plugins** (top toggle)
4. Click **Configure** on the Telegram card
5. Set:
   - Bot Token: `${TELEGRAM_BOT_TOKEN}`
   - Chat ID: your chat ID number
   - Parse Mode: `Markdown` (default)
6. Click **Save Configuration**
7. Enable the Telegram toggle

## 4. Add notification rules

In the Telegram card's rules section:
1. Click **+ Add Rule** → select **Task Failed**
2. Click **+ Add Rule** → select **Needs Approval**

## 5. Test it

Click **Test** on the Telegram card. You should receive a message in your chat.

That's it. You'll now get Telegram notifications whenever a task fails or needs approval.
