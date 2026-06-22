# Create a Custom Theme

1. Open **Plugins** in the Phoenix sidebar
2. Enable **Community Plugins** (top toggle)
3. Click the **Themes** tab
4. Click **+ Create Custom Theme**
5. Enter a name and select Dark or Light
6. Adjust the 15 color values using the color pickers
7. Check the preview dots (they show your background, accent, and surface colors)
8. Click **Save Theme**

Your theme is stored in Phoenix's database and will survive restarts and updates.

## Selecting your theme

Open the theme picker in the sidebar footer and select your new theme. Community themes are listed alongside the built-in ones.

## Sharing a theme

You can share your theme as a JSON file. Use the API to export it:

```bash
curl http://localhost:8080/api/plugins?type=theme | jq '.[] | select(.name == "My Theme")'
```

Others can import it via:

```bash
curl -X POST http://localhost:8080/api/plugins \
  -H 'Content-Type: application/json' \
  -d '<paste the JSON here>'
```
