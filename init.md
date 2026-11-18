I want a project that helps me view my photos. My problem is that I have a lot of photos on my computer, and I don't know which ones I have already seen—using Google Photos or similar tools—so I want to build my own app that remembers how many times I have viewed each photo.

What I envision is a web app where I can browse my photos and track how many times I have viewed each one.

On the search page, I'd like a filter to select how many times a photo has been viewed.

Otherwise, it should work like a standard photo viewer: a page to find and filter photos, and when I click a photo, it expands so I can see the full photo and its details.

It should support many photo and video formats, such as JPG, CR3, HEIC, and others.

For the frontend, follow https://github.com/GoogleChrome/modern-web-guidance.

For the backend, use Go with strict struct separation: request/response structs (used only at the API boundary), domain objects (used across the application), and entity structs (representing database records). Feel free to suggest conventions or rules to keep the project well organized.

I want unit tests, integration tests, linting, and a max-file-size lint check.

For data storage, use SQLite stored in a location that is easy to back up and share.

Possible problems
* How to track when a photo changes—perhaps identify photos based on their file content? Add a button to re-import.

Maintainability
* Create documentation covering project organization and features.
