#!/bin/sh

# Verify OS
OS="$(uname)"
if [ "$OS" = "Darwin" ]; then
    OS="osx"
elif [ "$OS" = "Linux" ]; then
    OS="linux"
else
    echo "Kelda can only be installed on either MacOS or Linux."
    exit 1
fi

RELEASE="latest"
if [ -n "$1" ]; then
    RELEASE="$1"
    echo "Downloading Kelda release ${RELEASE}..."
else
    echo "Downloading the latest Kelda release..."
fi

# Download the latest Kelda release into a temporary directory
# Try cURL, then wget, otherwise fail
TEMPDIR=$(mktemp -d)
ENDPOINT="https://update.kelda.io/?file=kelda&release=${RELEASE}&token=install&os=$OS"
if which curl > /dev/null; then
    if ! curl -#fSLo "$TEMPDIR/kelda.tar.gz" "$ENDPOINT"; then
        echo "Failed to download Kelda...exiting"
        exit 1
    fi
elif which wget > /dev/null; then
    if ! wget -O "$TEMPDIR/kelda.tar.gz" "$ENDPOINT" ; then
        echo "Failed to download Kelda...exiting"
        exit 1
    fi
else
    echo "Installing Kelda requires either cURL or wget to be installed."
fi

if ! tar -xzf "$TEMPDIR/kelda.tar.gz" ; then
    echo "Failed to extract the Kelda release...exiting"
    exit 1
fi

chmod +x "./kelda"

echo
echo "The latest Kelda release has been downloaded to the current working directory."
echo

read -p "Copy the binary into /usr/local/bin? (y/N) " choice < /dev/tty
case "$choice" in
    y|Y )
        echo "You may be prompted for your sudo password in order to write to /usr/local/bin."
        if [ -d "/usr/local/bin" ]; then
            sudo -p 'Sudo password: ' -- mv ./kelda /usr/local/bin
        else
            sudo -p 'Sudo password: ' -- mkdir -p /usr/local/bin && sudo mv ./kelda /usr/local/bin
        fi

        echo
        echo "Successfully installed Kelda!"
        ;;
    * ) echo "You will have to move the binary into your PATH in order to invoke Kelda globally.";;
esac

echo "We recommend trying the demo with 'kelda dev --demo' if this is your first time using Kelda."
