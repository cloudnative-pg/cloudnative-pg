const fs = require('fs');
const path = require('path');

// --- CONFIGURATION ---
const DRY_RUN = false; // Set to 'false' to apply changes
const DOCS_DIR = path.join(__dirname, 'docs', 'src');

// --- MAPPING CONFIGURATION ---
// Maps MkDocs/Material types to standard Docusaurus types
const TYPE_MAPPING = {
  // Standard Docusaurus types (Direct mapping)
  'note': 'note',
  'tip': 'tip',
  'info': 'info',
  'warning': 'warning',
  'danger': 'danger',

  // MkDocs Material Extra types
  'abstract': 'note',    // Abstract -> Note
  'success': 'tip',      // Success -> Tip (Green)
  'question': 'info',    // Question -> Info (Blue)
  'failure': 'danger',   // Failure -> Danger (Red)
  'bug': 'danger',       // Bug -> Danger (Red)
  'example': 'note',     // Example -> Note (Grey)
  'quote': 'note',       // Quote -> Note (Grey)
  
  // Common Custom types often used in docs
  'hint': 'tip',         // Hint -> Tip
  'important': 'info',   // Important -> Info
  'caution': 'warning',  // Caution -> Warning
  'tldr': 'note'         // TL;DR -> Note
};

// --- HELPER FUNCTIONS ---

function findAllMarkdownFiles(dirPath) {
  const filesList = [];
  try {
    const entries = fs.readdirSync(dirPath, { withFileTypes: true });
    for (const entry of entries) {
      const fullPath = path.join(dirPath, entry.name);
      if (entry.isDirectory()) {
        filesList.push(...findAllMarkdownFiles(fullPath));
      } else if (entry.isFile() && entry.name.endsWith('.md')) {
        filesList.push(fullPath);
      }
    }
  } catch (err) {
    console.error(`[ERROR] Could not read directory: ${dirPath}`, err);
  }
  return filesList;
}

/**
 * Helper to capitalize first letter (for default titles)
 */
function capitalize(str) {
  return str.charAt(0).toUpperCase() + str.slice(1);
}

function processContent(content) {
  const lines = content.split(/\r?\n/);
  const newLines = [];
  
  // Stack to keep track of open admonitions.
  const stack = [];

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    
    // 1. CHECK FOR START OF ADMONITION
    // Regex: Start of line, optional indent, !!!, whitespace, type, optional quoted title
    const startMatch = line.match(/^(\s*)!!!\s+(\w+)(?:\s+"(.*?)")?/);

    if (startMatch) {
      const [_, indentStr, rawType, rawTitle] = startMatch;
      const currentIndent = indentStr.length;
      const lowerType = rawType.toLowerCase();

      // Resolve the Docusaurus type. Default to 'note' if unknown.
      const docusaurusType = TYPE_MAPPING[lowerType] || 'note';

      // LOGIC: Handle the Title
      // 1. If rawTitle exists (e.g. !!! bug "My Bug"), use "My Bug"
      // 2. If no title, but we CHANGED the type (e.g. bug -> danger), auto-generate title "Bug"
      // 3. If no title and type is same (e.g. note -> note), leave empty (Docusaurus uses default "Note")
      
      let finalTitle = rawTitle;
      
      if (!finalTitle && docusaurusType !== lowerType) {
        // Preserve semantic meaning. 
        // e.g. !!! bug -> :::danger[Bug]
        finalTitle = capitalize(rawType);
      }
      
      // Close any blocks that have ended based on indentation
      while (stack.length > 0) {
        const top = stack[stack.length - 1];
        if (currentIndent <= top.baseIndent) {
           const closingBlock = stack.pop();
           const closeIndent = ' '.repeat(closingBlock.baseIndent);
           const colons = ':'.repeat(3 + stack.length);
           newLines.push(`${closeIndent}${colons}`);
        } else {
          break;
        }
      }

      // Determine nesting level
      const colons = ':'.repeat(3 + stack.length);
      
      // Build the tag: :::type or :::type[Title]
      let openTag = `${indentStr}${colons}${docusaurusType}`;
      if (finalTitle) {
        openTag += `[${finalTitle}]`;
      }

      newLines.push(openTag);
      
      stack.push({
        baseIndent: currentIndent,
        contentIndent: currentIndent + 1
      });
      continue; 
    }

    // 2. CHECK FOR END OF ADMONITION (by indentation drop)
    if (stack.length > 0 && line.trim() !== '') {
      const currentLineIndent = line.match(/^\s*/)[0].length;
      
      while (stack.length > 0) {
        const top = stack[stack.length - 1];
        if (currentLineIndent < top.contentIndent) {
          const closingBlock = stack.pop();
          const closeIndent = ' '.repeat(closingBlock.baseIndent);
          const colons = ':'.repeat(3 + stack.length);
          newLines.push(`${closeIndent}${colons}`);
        } else {
          break;
        }
      }
    }

    // 3. REGULAR CONTENT
    newLines.push(line);
  }

  // 4. CLEANUP END OF FILE
  while (stack.length > 0) {
    const closingBlock = stack.pop();
    const closeIndent = ' '.repeat(closingBlock.baseIndent);
    const colons = ':'.repeat(3 + stack.length);
    newLines.push(`${closeIndent}${colons}`);
  }

  return newLines.join('\n');
}

console.log(`Starting Admonition Conversion with Type Mapping...`);
if (DRY_RUN) {
  console.log("!!! DRY RUN MODE: No files will be changed !!!");
}

const files = findAllMarkdownFiles(DOCS_DIR);
let changedCount = 0;

files.forEach(filePath => {
  const content = fs.readFileSync(filePath, 'utf8');
  
  if (!content.includes('!!!')) {
    return;
  }

  const newContent = processContent(content);

  if (content !== newContent) {
    changedCount++;
    if (DRY_RUN) {
      console.log(`[DRY RUN] Would convert admonitions in: ${path.basename(filePath)}`);
    } else {
      fs.writeFileSync(filePath, newContent, 'utf8');
      console.log(`[UPDATED] ${path.basename(filePath)}`);
    }
  }
});

console.log(`-----------------------------`);
console.log(`Process complete. ${changedCount} files updated.`);
if (DRY_RUN) console.log("Set DRY_RUN = false to apply changes.");