#!/bin/bash

# Script to run journalist API tests with environment variables
# Usage: ./run_test.sh [journalist_type]
# Examples:
#   ./run_test.sh body_mind
#   ./run_test.sh feature
#   ./run_test.sh interview
#   ./run_test.sh general

# Default to body_mind if no parameter provided
JOURNALIST_TYPE=${1:-body_mind}

# Map journalist types to test files
case $JOURNALIST_TYPE in
    "body_mind")
        TEST_FILE="test_body_mind_real.go"
        TEST_NAME="Body and Mind Columnist"
        ;;
    "feature")
        TEST_FILE="test_feature_real.go"
        TEST_NAME="Feature Writer"
        ;;
    "interview")
        TEST_FILE="test_interview_real.go"
        TEST_NAME="Interview Specialist"
        ;;
    "general")
        TEST_FILE="test_general_real.go"
        TEST_NAME="General/Staff Reporter"
        ;;
    *)
        echo "❌ Unknown journalist type: $JOURNALIST_TYPE"
        echo ""
        echo "🎯 Available types:"
        echo "   - body_mind   (Body and Mind Columnist)"
        echo "   - feature     (Feature Writer)"
        echo "   - interview   (Interview Specialist)"
        echo "   - general     (General/Staff Reporter)"
        echo ""
        echo "📝 Usage: ./run_test.sh [journalist_type]"
        echo "📝 Example: ./run_test.sh feature"
        exit 1
        ;;
esac

echo "🧪 $TEST_NAME API Test"
echo "======================================"

# Check if test file exists
if [ ! -f "$TEST_FILE" ]; then
    echo "❌ Test file not found: $TEST_FILE"
    exit 1
fi

# Check if .env.local exists
if [ -f ".env.local" ]; then
    echo "📁 Loading environment from .env.local..."
    export $(cat .env.local | grep -v '^#' | xargs)
else
    echo "⚠️  .env.local not found - you can:"
    echo "   1. Create .env.local with your ANTHROPIC_API_KEY"
    echo "   2. Or set ANTHROPIC_API_KEY directly: export ANTHROPIC_API_KEY='your-key'"
    echo ""
fi

# Check if API key is set
if [ -z "$ANTHROPIC_API_KEY" ]; then
    echo "❌ ANTHROPIC_API_KEY not found!"
    echo ""
    echo "💡 Get your API key from: https://console.anthropic.com/account/keys"
    echo "💡 Then either:"
    echo "   - Add it to .env.local file"
    echo "   - Run: export ANTHROPIC_API_KEY='your-key-here'"
    echo ""
    exit 1
fi

echo "✅ API key found: ${ANTHROPIC_API_KEY:0:8}..."
echo ""

# Run the test
echo "🚀 Running $TEST_NAME test..."
go run "$TEST_FILE"