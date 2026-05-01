#!/bin/bash
# AnubisWatch Release Script
# ═══════════════════════════════════════════════════════════

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Script directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

# Default values
VERSION=""
DRY_RUN=false
SKIP_TESTS=false
SKIP_BUILD=false

# Usage
usage() {
    echo "AnubisWatch Release Script"
    echo ""
    echo "Usage: $0 [OPTIONS]"
    echo ""
    echo "Options:"
    echo "  -v, --version VERSION   Version to release (e.g., v1.0.0)"
    echo "  -d, --dry-run          Perform a dry run without making changes"
    echo "  -s, --skip-tests       Skip running tests"
    echo "  -b, --skip-build       Skip building binaries"
    echo "  -h, --help             Show this help message"
    echo ""
    echo "Examples:"
    echo "  $0 --version v1.0.0"
    echo "  $0 --version v1.0.0-rc1 --dry-run"
}

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -v|--version)
            VERSION="$2"
            shift 2
            ;;
        -d|--dry-run)
            DRY_RUN=true
            shift
            ;;
        -s|--skip-tests)
            SKIP_TESTS=true
            shift
            ;;
        -b|--skip-build)
            SKIP_BUILD=true
            shift
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            usage
            exit 1
            ;;
    esac
done

# Validate version
if [[ -z "$VERSION" ]]; then
    echo -e "${RED}Error: Version is required${NC}"
    usage
    exit 1
fi

# Validate version format
if [[ ! "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9]+)?$ ]]; then
    echo -e "${RED}Error: Invalid version format. Expected: vX.Y.Z or vX.Y.Z-alpha${NC}"
    exit 1
fi

echo "⚖️  AnubisWatch Release Script"
echo "═══════════════════════════════"
echo ""
echo "Version:   $VERSION"
echo "Dry Run:   $DRY_RUN"
echo "Skip Tests: $SKIP_TESTS"
echo "Skip Build: $SKIP_BUILD"
echo ""

# Check prerequisites
echo "📋 Checking prerequisites..."

# Check if git is clean
if [[ -n $(git status --porcelain) ]]; then
    echo -e "${RED}Error: Working directory is not clean${NC}"
    echo "Please commit or stash your changes before releasing."
    exit 1
fi

# Check if we're on main branch
CURRENT_BRANCH=$(git branch --show-current)
if [[ "$CURRENT_BRANCH" != "main" ]]; then
    echo -e "${YELLOW}Warning: Not on main branch (currently on $CURRENT_BRANCH)${NC}"
    read -p "Continue anyway? (y/N) " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        exit 1
    fi
fi

# Check if version tag already exists
if git rev-parse "$VERSION" >/dev/null 2>&1; then
    echo -e "${RED}Error: Tag $VERSION already exists${NC}"
    exit 1
fi

# Check tools
command -v go >/dev/null 2>&1 || { echo -e "${RED}Error: Go is required${NC}"; exit 1; }
command -v git >/dev/null 2>&1 || { echo -e "${RED}Error: Git is required${NC}"; exit 1; }

echo -e "${GREEN}✓ Prerequisites OK${NC}"
echo ""

# Run tests
if [[ "$SKIP_TESTS" == false ]]; then
    echo "🧪 Running tests..."
    cd "$PROJECT_ROOT"
    if ! go test -race -short ./...; then
        echo -e "${RED}Error: Tests failed${NC}"
        exit 1
    fi
    echo -e "${GREEN}✓ Tests passed${NC}"
    echo ""
fi

# Build binaries
if [[ "$SKIP_BUILD" == false ]]; then
    echo "🔨 Building binaries..."
    cd "$PROJECT_ROOT"

    # Build for all platforms
    make build-all || {
        echo -e "${RED}Error: Build failed${NC}"
        exit 1
    }

    echo -e "${GREEN}✓ Build complete${NC}"
    echo ""
fi

# Generate changelog
if [[ "$DRY_RUN" == false ]]; then
    echo "📝 Generating changelog..."

    # Get previous tag
    PREVIOUS_TAG=$(git describe --tags --abbrev=0 HEAD~1 2>/dev/null || echo "")

    if [[ -n "$PREVIOUS_TAG" ]]; then
        echo "Changes since $PREVIOUS_TAG:"
        git log --pretty=format:"- %s" "$PREVIOUS_TAG"..HEAD
    else
        echo "No previous tag found"
    fi

    echo ""
    echo -e "${GREEN}✓ Changelog generated${NC}"
    echo ""
fi

# Create git tag
if [[ "$DRY_RUN" == false ]]; then
    echo "🏷️  Creating git tag..."

    # Create tag
    git tag -a "$VERSION" -m "Release $VERSION"

    echo -e "${GREEN}✓ Tag created: $VERSION${NC}"
    echo ""

    # Push
    read -p "Push to origin? (y/N) " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        git push origin main
        git push origin "$VERSION"
        echo -e "${GREEN}✓ Pushed to origin${NC}"
    else
        echo -e "${YELLOW}⚠️  Not pushed. Run: git push origin main && git push origin $VERSION${NC}"
    fi

    echo ""
fi

# Summary
echo "═══════════════════════════════"
echo -e "${GREEN}✓ Release $VERSION prepared successfully!${NC}"
echo ""
echo "Next steps:"
if [[ "$DRY_RUN" == true ]]; then
    echo "  1. Remove --dry-run to perform actual release"
else
    echo "  1. Check the GitHub Actions workflow for build status"
    echo "  2. Verify the release artifacts"
    echo "  3. Publish the release on GitHub"
fi
echo ""
echo "⚖️  The Judgment Never Sleeps"
